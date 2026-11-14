package embed

import (
	"context"
	"fmt"
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
	return newRerankClient(baseURL, apiKey, model, false)
}

// cappedReranker limits how many candidate documents reach the rerank
// endpoint (rerank.top_n). Truncation is a prefix cut, so the indices the
// endpoint returns still line up with the caller's full candidate slice.
type cappedReranker struct {
	inner Reranker
	topN  int
}

func (c *cappedReranker) Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error) {
	if c.topN > 0 && len(documents) > c.topN {
		documents = documents[:c.topN]
	}
	return c.inner.Rerank(ctx, query, documents, topN)
}

func newRerankClient(baseURL, apiKey, model string, offline bool) *RerankClient {
	return &RerankClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    newHTTPClient(30*time.Second, offline, baseURL),
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

	endpoint := c.baseURL
	if !strings.HasSuffix(endpoint, "/rerank") {
		endpoint += "/rerank"
	}

	var parsedResp rerankResponse
	if err := postJSON(ctx, c.http, endpoint, c.apiKey, "rerank", reqPayload, &parsedResp); err != nil {
		return nil, err
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
