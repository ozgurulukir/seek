package embed

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
)

// DefaultVLEndpoint is the DashScope multimodal embedding endpoint, used when
// no custom VL base URL is configured.
const DefaultVLEndpoint = config.DefaultVLBaseURL

const (
	vlMaxContents = config.DefaultVLMaxContents // max content elements per request
	vlMaxImages   = config.DefaultVLMaxImages   // max images per request
)

// EmbedItem represents a single item to embed — either text-only or image+context.
type EmbedItem struct {
	Text     string // required for text chunks
	ImageURI string // optional: "data:image/png;base64,..." for image chunks
}

// VLClient is a client for a multimodal embedding API (vision-language models,
// e.g. DashScope qwen3-vl-embedding or any compatible endpoint).
type VLClient struct {
	apiKey     string
	model      string
	dimensions int
	endpoint   string
	taskPrefix TaskPrefix
	http       *http.Client
}

// NewVLClient creates a new multimodal embedding client. If endpoint is empty,
// DefaultVLEndpoint (DashScope) is used. The task prefix is applied to query
// texts (EmbedText) and document texts (EmbedBatch) like the text Client.
func NewVLClient(apiKey, model string, dimensions int, endpoint string, taskPrefix TaskPrefix) *VLClient {
	if endpoint == "" {
		endpoint = DefaultVLEndpoint
	}
	return &VLClient{
		apiKey:     apiKey,
		model:      model,
		dimensions: dimensions,
		endpoint:   endpoint,
		taskPrefix: taskPrefix,
		http:       &http.Client{Timeout: config.DefaultVLTimeout},
	}
}

// vlRequest is the DashScope multimodal embedding request format.
type vlRequest struct {
	Model      string   `json:"model"`
	Input      vlInput  `json:"input"`
	Parameters vlParams `json:"parameters"`
}

type vlInput struct {
	Contents []vlContent `json:"contents"`
}

type vlContent map[string]string // {"text": "..."} or {"image": "data:..."} or {"text": "...", "image": "data:..."}

type vlParams struct {
	Dimension int `json:"dimension"`
}

// vlResponse is the DashScope multimodal embedding response format.
type vlResponse struct {
	Output struct {
		Embeddings []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"embeddings"`
	} `json:"output"`
	Usage struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// EmbedText embeds a search query via the VL API, prepending the configured
// query task prefix.
func (c *VLClient) EmbedText(text string) ([]float32, error) {
	results, err := c.embedRaw([]EmbedItem{{Text: c.taskPrefix.applyQuery(text)}})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return results[0], nil
}

// EmbedImage embeds an image with optional context text.
func (c *VLClient) EmbedImage(imageDataURI string, context string) ([]float32, error) {
	item := EmbedItem{
		Text:     context,
		ImageURI: imageDataURI,
	}
	results, err := c.EmbedBatch([]EmbedItem{item})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return results[0], nil
}

// EmbedBatch embeds document items, prepending the configured document task
// prefix to each item's text (image context included). It automatically
// splits into sub-batches respecting the 20-content and 5-image limits per
// request.
func (c *VLClient) EmbedBatch(items []EmbedItem) ([][]float32, error) {
	if c.taskPrefix.Document != "" {
		prefixed := make([]EmbedItem, len(items))
		copy(prefixed, items)
		for i := range prefixed {
			if (prefixed[i].Text != "" || prefixed[i].ImageURI == "") && !strings.HasPrefix(prefixed[i].Text, c.taskPrefix.Document) {
				prefixed[i].Text = c.taskPrefix.Document + prefixed[i].Text
			}
		}
		items = prefixed
	}
	return c.embedRaw(items)
}

// embedRaw batches and sends items without any task-prefix transformation.
func (c *VLClient) embedRaw(items []EmbedItem) ([][]float32, error) {
	if len(items) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(items))

	// Split items into batches respecting limits
	batches := splitIntoBatches(items)

	globalOffset := 0
	for _, batch := range batches {
		embeddings, err := c.doRequest(batch)
		if err != nil {
			return nil, fmt.Errorf("vl batch at offset %d: %w", globalOffset, err)
		}

		for i, emb := range embeddings {
			if globalOffset+i < len(results) {
				results[globalOffset+i] = emb
			}
		}
		globalOffset += len(batch)
	}

	return results, nil
}

// splitIntoBatches splits items into batches respecting vlMaxContents and vlMaxImages limits.
func splitIntoBatches(items []EmbedItem) [][]EmbedItem {
	var batches [][]EmbedItem
	var currentBatch []EmbedItem
	imageCount := 0

	for _, item := range items {
		isImage := item.ImageURI != ""

		// Check if adding this item would exceed limits
		if len(currentBatch) >= vlMaxContents || (isImage && imageCount >= vlMaxImages) {
			if len(currentBatch) > 0 {
				batches = append(batches, currentBatch)
			}
			currentBatch = nil
			imageCount = 0
		}

		currentBatch = append(currentBatch, item)
		if isImage {
			imageCount++
		}
	}

	if len(currentBatch) > 0 {
		batches = append(batches, currentBatch)
	}

	return batches
}

// doRequest sends a single batch request to the VL API.
func (c *VLClient) doRequest(items []EmbedItem) ([][]float32, error) {
	var contents []vlContent

	for _, item := range items {
		content := make(vlContent)
		if item.ImageURI != "" {
			content["image"] = item.ImageURI
			if item.Text != "" {
				content["text"] = item.Text
			}
		} else {
			text := item.Text
			if text == "" {
				text = " " // VL API requires non-empty text
			}
			content["text"] = text
		}
		contents = append(contents, content)
	}

	req := vlRequest{
		Model: c.model,
		Input: vlInput{Contents: contents},
		Parameters: vlParams{
			Dimension: c.dimensions,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vl request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vl API %d: %s", resp.StatusCode, string(respBody))
	}

	var vlResp vlResponse
	if err := json.Unmarshal(respBody, &vlResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if vlResp.Code != "" {
		return nil, fmt.Errorf("vl API error [%s]: %s", vlResp.Code, vlResp.Message)
	}

	// Re-order by index
	result := make([][]float32, len(items))
	for _, e := range vlResp.Output.Embeddings {
		if e.Index >= 0 && e.Index < len(result) {
			result[e.Index] = e.Embedding
		}
	}

	return result, nil
}

// ImageToDataURI reads an image file and returns a data URI string.
func ImageToDataURI(imagePath string, mediaType string) (string, error) {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image %s: %w", imagePath, err)
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mediaType, b64), nil
}

// ImagePathToMediaType infers media type from file extension.
func ImagePathToMediaType(path string) string {
	ext := ""
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			ext = path[i+1:]
			break
		}
	}
	switch ext {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

// EmbedTextsInBatches embeds text strings in batches with rate-limiting pauses.
func (c *VLClient) EmbedTextsInBatches(texts []string, batchSize int, pause time.Duration, onBatch func(batchStart int, embeddings [][]float32) error) (int, error) {
	if batchSize <= 0 {
		batchSize = 20
	}
	updated := 0
	var firstErr error
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		items := make([]EmbedItem, end-i)
		for j := i; j < end; j++ {
			items[j-i] = EmbedItem{Text: texts[j]}
		}

		embeddings, err := c.EmbedBatch(items)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("text batch %d-%d: %w", i, end, err)
			}
			continue
		}

		if onBatch != nil {
			if err := onBatch(i, embeddings); err != nil {
				return updated, err
			}
		}
		for _, emb := range embeddings {
			if emb != nil {
				updated++
			}
		}

		if end < len(texts) && pause > 0 {
			time.Sleep(pause)
		}
	}
	return updated, firstErr
}

// ImageBatchItem pairs an image path with optional context text.
type ImageBatchItem struct {
	ImagePath string
	Text      string
}

// EmbedImagesInBatches reads images concurrently, formats data URIs, and embeds them in batches.
func (c *VLClient) EmbedImagesInBatches(items []ImageBatchItem, batchSize int, pause time.Duration, onBatch func(batchStart int, embeddings [][]float32, validIndices []int) error) (int, error) {
	if batchSize <= 0 {
		batchSize = 5
	}
	updated := 0
	var firstErr error
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}

		type readResult struct {
			dataURI string
			err     error
		}
		results := make([]readResult, end-i)
		var wg sync.WaitGroup

		for j := i; j < end; j++ {
			wg.Add(1)
			go func(idx int, item ImageBatchItem) {
				defer wg.Done()
				mediaType := ImagePathToMediaType(item.ImagePath)
				dataURI, err := ImageToDataURI(item.ImagePath, mediaType)
				results[idx-i] = readResult{dataURI, err}
			}(j, items[j])
		}
		wg.Wait()

		embedItems := make([]EmbedItem, 0, end-i)
		validIndices := make([]int, 0, end-i)

		for j := i; j < end; j++ {
			item := items[j]
			res := results[j-i]
			if res.err != nil {
				continue
			}
			embedItems = append(embedItems, EmbedItem{
				Text:     item.Text,
				ImageURI: res.dataURI,
			})
			validIndices = append(validIndices, j)
		}

		if len(embedItems) == 0 {
			continue
		}

		embeddings, err := c.EmbedBatch(embedItems)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("image batch %d-%d: %w", i, end, err)
			}
			continue
		}

		if onBatch != nil {
			if err := onBatch(i, embeddings, validIndices); err != nil {
				return updated, err
			}
		}
		for _, emb := range embeddings {
			if emb != nil {
				updated++
			}
		}

		if end < len(items) && pause > 0 {
			time.Sleep(pause)
		}
	}
	return updated, firstErr
}
