package embed_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
)

func TestRerankClient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		var req struct {
			Model     string   `json:"model"`
			Query     string   `json:"query"`
			Documents []string `json:"documents"`
			TopN      int      `json:"top_n"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if req.Query != "search query" {
			t.Errorf("unexpected query: %s", req.Query)
		}
		if len(req.Documents) != 2 {
			t.Errorf("expected 2 documents, got %d", len(req.Documents))
		}

		resp := map[string]interface{}{
			"results": []map[string]interface{}{
				{"index": 1, "relevance_score": 0.95},
				{"index": 0, "relevance_score": 0.35},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := embed.NewRerankClient(ts.URL, "test-key", "bge-reranker-large")
	results, err := client.Rerank(context.Background(), "search query", []string{"doc 0", "doc 1"}, 2)
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Index != 1 || results[0].RelevanceScore != 0.95 {
		t.Errorf("unexpected first result: %+v", results[0])
	}
	if results[1].Index != 0 || results[1].RelevanceScore != 0.35 {
		t.Errorf("unexpected second result: %+v", results[1])
	}
}

// TestRerankTopNCapsCandidatePool pins the rerank.top_n wiring: the provider
// wraps its reranker so only the first top_n candidates reach the endpoint,
// while the returned indices stay aligned with the caller's full slice.
func TestRerankTopNCapsCandidatePool(t *testing.T) {
	var gotDocs atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Documents []string `json:"documents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotDocs.Store(int32(len(req.Documents)))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"index":1,"relevance_score":0.9}]}`)
	}))
	defer ts.Close()

	cfg := &config.AppConfig{Config: config.Config{
		Rerank: config.RerankConfig{Enabled: true, BaseURL: ts.URL, APIKey: "k", Model: "m", TopN: 2},
	}}
	provider, err := embed.NewProviderFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewProviderFromConfig: %v", err)
	}
	if provider.Reranker == nil {
		t.Fatal("configured rerank provider has no reranker")
	}

	results, err := provider.Reranker.Rerank(context.Background(), "q", []string{"a", "b", "c", "d", "e"}, 10)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if got := gotDocs.Load(); got != 2 {
		t.Errorf("endpoint received %d documents, want 2 (rerank.top_n cap)", got)
	}
	if len(results) != 1 || results[0].Index != 1 {
		t.Errorf("unexpected results: %+v (index must map into the caller's full slice)", results)
	}
}
