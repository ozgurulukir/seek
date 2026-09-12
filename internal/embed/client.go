package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
)

// TaskPrefix holds model-specific input prefixes for asymmetric embedding
// models (e.g. Nomic's "search_query: "/"search_document: "). The zero
// value applies no prefixes.
type TaskPrefix struct {
	Query    string
	Document string
}

func (p TaskPrefix) applyQuery(text string) string {
	if p.Query != "" && !strings.HasPrefix(text, p.Query) {
		return p.Query + text
	}
	return text
}

// applyDocuments returns texts with the document prefix prepended. When no
// prefix is configured or texts already start with the prefix, it is not duplicated.
func (p TaskPrefix) applyDocuments(texts []string) []string {
	if p.Document == "" {
		return texts
	}
	out := make([]string, len(texts))
	for i, t := range texts {
		if !strings.HasPrefix(t, p.Document) {
			out[i] = p.Document + t
		} else {
			out[i] = t
		}
	}
	return out
}

type Client struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	taskPrefix TaskPrefix
	http       *http.Client
	// offline marks a client that must not perform any network call
	// (search.privacy.offline_only). Requests fail fast, before any chunk,
	// query, or image text leaves the machine.
	offline bool
}

// NewClient builds a client for any OpenAI-compatible embeddings endpoint.
// Set offline to refuse all network calls (config privacy.offline_only).
func NewClient(baseURL, apiKey, model string, dimensions int, taskPrefix TaskPrefix) *Client {
	return newClient(baseURL, apiKey, model, dimensions, taskPrefix, false)
}

// NewOfflineClient returns a client whose every request fails immediately
// with a clear error — used when privacy.offline_only is set.
func NewOfflineClient(model string) *Client {
	return newClient("", "", model, 0, TaskPrefix{}, true)
}

func newClient(baseURL, apiKey, model string, dimensions int, taskPrefix TaskPrefix, offline bool) *Client {
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		dimensions: dimensions,
		taskPrefix: taskPrefix,
		http:       newHTTPClient(config.DefaultEmbeddingTimeout, offline),
		offline:    offline,
	}
}

// newHTTPClient applies the additional transport restrictions required by
// offline-only mode. The default path deliberately keeps Go's normal proxy
// and redirect behavior for remote providers.
func newHTTPClient(timeout time.Duration, offline bool) *http.Client {
	client := &http.Client{Timeout: timeout}
	if !offline {
		return client
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		transport = transport.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.Proxy = nil
	client.Transport = transport
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

type embeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// embed sends the raw texts to the embeddings endpoint without any
// task-prefix transformation. Public methods layer prefixes on top.
func (c *Client) embed(texts []string) ([][]float32, error) {
	return c.embedContext(context.Background(), texts)
}

func (c *Client) embedContext(ctx context.Context, texts []string) ([][]float32, error) {
	if c.offline {
		return nil, fmt.Errorf("offline_only is enabled: refusing to send %d text(s) to %q", len(texts), c.model)
	}
	req := embeddingRequest{
		Model: c.model,
		Input: texts,
	}
	if c.dimensions > 0 {
		req.Dimensions = c.dimensions
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API %d: %s", resp.StatusCode, string(respBody))
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if embResp.Error != nil {
		return nil, fmt.Errorf("embedding error: %s", embResp.Error.Message)
	}

	if len(embResp.Data) != len(texts) {
		return nil, fmt.Errorf("embedding API returned %d embeddings, expected %d", len(embResp.Data), len(texts))
	}

	// Re-order by index
	result := make([][]float32, len(texts))
	for _, d := range embResp.Data {
		if d.Index < 0 || d.Index >= len(result) {
			return nil, fmt.Errorf("embedding API returned index %d out of range (expected 0..%d)", d.Index, len(result)-1)
		}
		result[d.Index] = d.Embedding
	}
	// Duplicate indexes would satisfy the count check while leaving nil holes.
	for i, emb := range result {
		if len(emb) == 0 {
			return nil, fmt.Errorf("embedding API returned no embedding for input %d", i)
		}
	}

	return result, nil
}

// EmbedDocuments embeds indexed document texts, prepending the configured
// document task prefix (e.g. "search_document: " for Nomic models).
func (c *Client) EmbedDocuments(texts []string) ([][]float32, error) {
	return c.embed(c.taskPrefix.applyDocuments(texts))
}

func (c *Client) EmbedDocumentsContext(ctx context.Context, texts []string) ([][]float32, error) {
	return c.embedContext(ctx, c.taskPrefix.applyDocuments(texts))
}

// EmbedQuery embeds a search query, prepending the configured query task
// prefix (e.g. "search_query: " for Nomic models).
func (c *Client) EmbedQuery(text string) ([]float32, error) {
	return c.EmbedQueryContext(context.Background(), text)
}

func (c *Client) EmbedQueryContext(ctx context.Context, text string) ([]float32, error) {
	results, err := c.embedContext(ctx, []string{c.taskPrefix.applyQuery(text)})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return results[0], nil
}

// BatchEmbed handles document texts in batches of batchSize, applying the
// document task prefix like EmbedDocuments.
func (c *Client) BatchEmbed(texts []string, batchSize int) ([][]float32, error) {
	if batchSize <= 0 {
		batchSize = config.DefaultEmbeddingBatchSize
	}

	all := make([][]float32, len(texts))
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := c.EmbedDocuments(texts[i:end])
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d: %w", i, end, err)
		}
		copy(all[i:end], batch)
	}
	return all, nil
}
