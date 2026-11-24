package embed

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
)

// WarnLogger is the batch downloader's logging seam. Library code must not
// write timestamped stderr lines via the stdlib log — the caller owns the
// output channel (pipeline.Logger and indexer.Logger satisfy it
// structurally), and a nil logger discards, keeping the batch path silent
// for embedded consumers. This is the C1 audit fix: the WARN summary for
// partial batch downloads used to go straight to stdlib log.
type WarnLogger interface {
	Printf(format string, v ...any)
}

// BatchJob tracks a batch embedding job.
type BatchJob struct {
	ID           string
	Status       string
	OutputFileID string
	ErrorFileID  string
}

// batchRequestLine is one line in the batch input JSONL.
type batchRequestLine struct {
	CustomID string                 `json:"custom_id"`
	Method   string                 `json:"method"`
	URL      string                 `json:"url"`
	Body     map[string]interface{} `json:"body"`
}

// batchResponseLine is one line in the batch output JSONL.
type batchResponseLine struct {
	CustomID string `json:"custom_id"`
	Response struct {
		StatusCode int             `json:"status_code"`
		Body       json.RawMessage `json:"body"`
	} `json:"response"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// PrepareBatchJSONL creates the JSONL content for batch embedding.
// Each chunk gets a custom_id of "chunk-{index}". The document task prefix
// is applied to every input, matching EmbedDocuments.
func (c *Client) PrepareBatchJSONL(texts []string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	for i, text := range texts {
		docInput := text
		if c.taskPrefix.Document != "" && !strings.HasPrefix(text, c.taskPrefix.Document) {
			docInput = c.taskPrefix.Document + text
		}
		line := batchRequestLine{
			CustomID: fmt.Sprintf("chunk-%d", i),
			Method:   "POST",
			URL:      "/v1/embeddings",
			Body: map[string]interface{}{
				"model": c.model,
				"input": docInput,
			},
		}
		if c.dimensions > 0 {
			line.Body["dimensions"] = c.dimensions
		}
		if err := enc.Encode(line); err != nil {
			return nil, fmt.Errorf("encode line %d: %w", i, err)
		}
	}
	return buf.Bytes(), nil
}

// UploadBatchFile uploads a JSONL file for batch processing.
func (c *Client) UploadBatchFile(jsonlData []byte) (string, error) {
	return c.UploadBatchFileContext(context.Background(), jsonlData)
}

func (c *Client) UploadBatchFileContext(ctx context.Context, jsonlData []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("purpose", "batch"); err != nil {
		return "", err
	}
	part, err := writer.CreateFormFile("file", "batch_embeddings.jsonl")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(jsonlData); err != nil {
		return "", err
	}
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/files", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return "", fmt.Errorf("upload failed %d: %s", resp.StatusCode, string(snippet))
	}

	respBody, err := readLimitedBody(resp.Body, maxBatchResponseBytes, "upload")
	if err != nil {
		return "", err
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse upload response: %w", err)
	}
	return result.ID, nil
}

// CreateBatch creates a batch job with the uploaded file.
func (c *Client) CreateBatch(fileID string) (*BatchJob, error) {
	return c.CreateBatchContext(context.Background(), fileID)
}

func (c *Client) CreateBatchContext(ctx context.Context, fileID string) (*BatchJob, error) {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"input_file_id":     fileID,
		"endpoint":          "/v1/embeddings",
		"completion_window": "24h",
	})

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/batches", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create batch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, fmt.Errorf("create batch failed %d: %s", resp.StatusCode, string(snippet))
	}

	respBody, err := readLimitedBody(resp.Body, maxBatchResponseBytes, "create batch")
	if err != nil {
		return nil, err
	}

	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	return &BatchJob{ID: result.ID, Status: result.Status}, nil
}

const maxBatchPollAttempts = 17280 // ~24 hours at 5s intervals

// PollBatch polls until the batch job completes. Returns the final job state.
// It caps at maxBatchPollAttempts to prevent infinite loops on unexpected statuses.
func (c *Client) PollBatch(batchID string, onStatus func(status string, elapsed time.Duration)) (*BatchJob, error) {
	return c.PollBatchContext(context.Background(), batchID, onStatus)
}

func (c *Client) PollBatchContext(ctx context.Context, batchID string, onStatus func(status string, elapsed time.Duration)) (*BatchJob, error) {
	start := time.Now()

	for attempt := 0; attempt < maxBatchPollAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/batches/"+batchID, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("poll batch: %w", err)
		}

		if resp.StatusCode != 200 {
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
			resp.Body.Close()
			return nil, fmt.Errorf("poll batch failed %d: %s", resp.StatusCode, string(snippet))
		}

		respBody, err := readLimitedBody(resp.Body, maxBatchResponseBytes, "poll")
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		var result struct {
			ID           string `json:"id"`
			Status       string `json:"status"`
			OutputFileID string `json:"output_file_id"`
			ErrorFileID  string `json:"error_file_id"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("parse poll response: %w", err)
		}

		job := &BatchJob{
			ID:           result.ID,
			Status:       result.Status,
			OutputFileID: result.OutputFileID,
			ErrorFileID:  result.ErrorFileID,
		}

		if onStatus != nil {
			onStatus(result.Status, time.Since(start))
		}

		switch result.Status {
		case "completed":
			return job, nil
		case "failed", "expired", "cancelled":
			return job, fmt.Errorf("batch %s: %s", result.Status, boundedErrorSnippet(respBody))
		}

		if err := sleepContext(ctx, config.DefaultBatchPollInterval); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("batch polling exceeded maximum attempts (%d)", maxBatchPollAttempts)
}

// sleepContext pauses for d unless ctx is canceled first, returning ctx.Err()
// in that case. It uses one explicitly stopped timer instead of time.After's
// untracked per-iteration timer in loops that can run for ~24h
// (review 2026-09-17 L13).
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// DownloadBatchResults downloads and parses the batch output file.
// Returns a map of custom_id -> embedding. Partial-failure warnings are
// discarded; pass a WarnLogger via DownloadBatchResultsContext to observe them.
func (c *Client) DownloadBatchResults(fileID string) (map[string][]float32, error) {
	return c.DownloadBatchResultsContext(context.Background(), fileID, nil)
}

func (c *Client) DownloadBatchResultsContext(ctx context.Context, fileID string, warn WarnLogger) (map[string][]float32, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/files/"+fileID+"/content", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, fmt.Errorf("download failed %d: %s", resp.StatusCode, string(snippet))
	}

	respBody, err := readLimitedBody(resp.Body, maxBatchResponseBytes, "download")
	if err != nil {
		return nil, err
	}

	results := make(map[string][]float32)
	// Scan line-by-line over the bounded body instead of converting it to a
	// string and splitting: for typical multi-line results this avoids a
	// second full-size allocation and a slice of every line. A single line at
	// the body cap can still grow the scanner buffer to that cap, so peak
	// memory for that pathological case is roughly twice the body size.
	scanner := bufio.NewScanner(bytes.NewReader(respBody))
	scanner.Buffer(make([]byte, 64*1024), maxBatchResponseBytes)

	var parseErrors, apiErrors, lines int
	for scanner.Scan() {
		lines++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var resp batchResponseLine
		if err := json.Unmarshal(line, &resp); err != nil {
			parseErrors++
			continue
		}
		if resp.Error != nil {
			apiErrors++
			continue
		}
		if resp.Response.StatusCode != 200 {
			apiErrors++
			continue
		}

		var embResp embeddingResponse
		if err := json.Unmarshal(resp.Response.Body, &embResp); err != nil {
			parseErrors++
			continue
		}
		if len(embResp.Data) > 0 {
			results[resp.CustomID] = embResp.Data[0].Embedding
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan download response: %w", err)
	}

	// Return partial results even when some items failed. Batch APIs commonly
	// have per-item failures; failing the entire download would discard valid
	// embeddings. The caller detects incomplete results by comparing
	// len(results) to the expected count.
	if parseErrors > 0 || apiErrors > 0 {
		if warn != nil {
			warn.Printf("WARN: batch download: %d parse errors, %d API errors out of %d lines",
				parseErrors, apiErrors, lines)
		}
	}
	return results, nil
}

// BatchEmbedAsyncContext runs the full batch embedding flow:
// prepare JSONL → upload → create batch → poll → download → return embeddings.
// warn receives the partial-failure summary from the result download; nil
// discards it.
func (c *Client) BatchEmbedAsyncContext(ctx context.Context, texts []string, onStatus func(status string, elapsed time.Duration), warn WarnLogger) ([][]float32, error) {
	if c.offline {
		return nil, fmt.Errorf("offline_only is enabled: refusing to send %d text(s) to %q", len(texts), c.model)
	}
	if len(texts) == 0 {
		return nil, nil
	}

	// 1. Prepare JSONL
	jsonl, err := c.PrepareBatchJSONL(texts)
	if err != nil {
		return nil, fmt.Errorf("prepare batch: %w", err)
	}

	// 2. Upload file
	fileID, err := c.UploadBatchFileContext(ctx, jsonl)
	if err != nil {
		return nil, fmt.Errorf("upload batch file: %w", err)
	}

	// 3. Create batch job
	job, err := c.CreateBatchContext(ctx, fileID)
	if err != nil {
		return nil, fmt.Errorf("create batch job: %w", err)
	}

	// 4. Poll until done
	job, err = c.PollBatchContext(ctx, job.ID, onStatus)
	if err != nil {
		return nil, err
	}

	if job.OutputFileID == "" {
		return nil, fmt.Errorf("no output file from batch")
	}

	// 5. Download results
	resultMap, err := c.DownloadBatchResultsContext(ctx, job.OutputFileID, warn)
	if err != nil {
		return nil, fmt.Errorf("download batch results: %w", err)
	}

	// 6. Reassemble in order
	return reassembleBatchEmbeddings(texts, resultMap)
}

// reassembleBatchEmbeddings maps downloaded batch results back onto the
// request order via their chunk-N keys. A missing key means that item failed
// provider-side; DownloadBatchResults deliberately returns partial results,
// but reporting success here would silently drop those chunks from the
// vector index, so incompleteness is an error — the DB keeps whatever was
// already embedded, and a re-run retries the rest (review 2026-09-17 M10).
func reassembleBatchEmbeddings(texts []string, resultMap map[string][]float32) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	missing := 0
	for i := range texts {
		if emb, ok := resultMap[fmt.Sprintf("chunk-%d", i)]; ok {
			embeddings[i] = emb
		} else {
			missing++
		}
	}
	if missing > 0 {
		return nil, fmt.Errorf("batch results incomplete: %d of %d embedding(s) missing; re-run to retry the missing chunks", missing, len(texts))
	}
	return embeddings, nil
}
