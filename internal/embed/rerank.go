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
)

// RerankResult contains the index of the candidate document and its cross-encoder relevance score.
type RerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// Reranker defines the interface for cross-encoder reranking models.
type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error)
}

// RerankClient communicates with standard OpenAI/Cohere/Jina/BGE compatible rerank endpoints.
type RerankClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// NewRerankClient creates a new rerank client.
func NewRerankClient(baseURL, apiKey, model string) *RerankClient {
	return &RerankClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

type rerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Rerank scores a candidate pool of document strings against the query using the cross-encoder model.
func (c *RerankClient) Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	if topN <= 0 || topN > len(documents) {
		topN = len(documents)
	}

	reqPayload := rerankRequest{
		Model:     c.model,
		Query:     query,
		Documents: documents,
		TopN:      topN,
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}

	endpoint := c.baseURL
	if !strings.HasSuffix(endpoint, "/rerank") {
		endpoint += "/rerank"
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("rerank request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank API %d: %s", resp.StatusCode, string(respBody))
	}

	var parsedResp rerankResponse
	if err := json.Unmarshal(respBody, &parsedResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if parsedResp.Error != nil {
		return nil, fmt.Errorf("rerank error: %s", parsedResp.Error.Message)
	}

	results := make([]RerankResult, len(parsedResp.Results))
	for i, r := range parsedResp.Results {
		results[i] = RerankResult{
			Index:          r.Index,
			RelevanceScore: r.RelevanceScore,
		}
	}
	return results, nil
}
