package embed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// newTestClient returns a Client whose HTTP transport points at the given
// handler, so tests can exercise the request/response contract without a
// real network call.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "test-key", "test-model", 8, TaskPrefix{})
	c.http = srv.Client()
	return c
}

func TestNewClient(t *testing.T) {
	c := NewClient("https://api.example/v1", "k", "m", 16, TaskPrefix{})
	if c.baseURL != "https://api.example/v1" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "https://api.example/v1")
	}
	if c.apiKey != "k" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "k")
	}
	if c.model != "m" {
		t.Errorf("model = %q, want %q", c.model, "m")
	}
	if c.dimensions != 16 {
		t.Errorf("dimensions = %d, want 16", c.dimensions)
	}
	if c.http == nil {
		t.Error("http client not initialized")
	}
}

func TestEmbedRequestShape(t *testing.T) {
	var gotBody embeddingRequest
	var gotAuth string
	var gotPath string

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.1, 0.2}, Index: 0},
			},
		})
	})

	_, err := c.EmbedDocuments([]string{"hello"})
	if err != nil {
		t.Fatalf("EmbedDocuments: %v", err)
	}

	if gotPath != "/embeddings" {
		t.Errorf("path = %q, want /embeddings", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
	if gotBody.Model != "test-model" {
		t.Errorf("model = %q, want test-model", gotBody.Model)
	}
	if !reflect.DeepEqual(gotBody.Input, []string{"hello"}) {
		t.Errorf("input = %v, want [hello]", gotBody.Input)
	}
	if gotBody.Dimensions != 8 {
		t.Errorf("dimensions = %d, want 8", gotBody.Dimensions)
	}
}

func TestEmbedOmitsDimensionsWhenZero(t *testing.T) {
	var gotBody embeddingRequest
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embeddingResponse{})
	})
	c.dimensions = 0

	if _, err := c.EmbedDocuments([]string{"x"}); err != nil {
		t.Fatalf("EmbedDocuments: %v", err)
	}
	if gotBody.Dimensions != 0 {
		t.Errorf("dimensions = %d, want 0 (omitted)", gotBody.Dimensions)
	}
}

func TestEmbedReordersByIndex(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return out of order to verify reordering by index.
		json.NewEncoder(w).Encode(embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{3, 3}, Index: 2},
				{Embedding: []float32{1, 1}, Index: 0},
				{Embedding: []float32{2, 2}, Index: 1},
			},
		})
	})

	got, err := c.EmbedDocuments([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("EmbedDocuments: %v", err)
	}
	want := [][]float32{{1, 1}, {2, 2}, {3, 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("embeddings = %v, want %v", got, want)
	}
}

func TestEmbedHTTPError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	})

	_, err := c.EmbedDocuments([]string{"a"})
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want status code mention", err.Error())
	}
}

func TestEmbedAPIErrorField(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(embeddingResponse{
			Error: &struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			}{Message: "rate limited", Type: "rate_limit"},
		})
	})

	_, err := c.EmbedDocuments([]string{"a"})
	if err == nil {
		t.Fatal("expected error for API error field")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %q, want message", err.Error())
	}
}

func TestEmbedQuery(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.5, 0.5}, Index: 0},
			},
		})
	})

	got, err := c.EmbedQuery("hello")
	if err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if !reflect.DeepEqual(got, []float32{0.5, 0.5}) {
		t.Errorf("embedding = %v, want [0.5 0.5]", got)
	}
}

func TestEmbedQueryNoResult(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embeddingResponse{})
	})

	// embed returns a len-1 slice with a nil inner slice when the API returns
	// no data, so EmbedQuery yields a nil embedding without error.
	got, err := c.EmbedQuery("hello")
	if err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if got != nil {
		t.Errorf("embedding = %v, want nil", got)
	}
}

func TestBatchEmbed(t *testing.T) {
	var batches [][]string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req embeddingRequest
		json.NewDecoder(r.Body).Decode(&req)
		batches = append(batches, req.Input)

		// Return an embedding derived from the text content so cross-batch
		// reassembly is verifiable (index restarts at 0 in each batch).
		w.Header().Set("Content-Type", "application/json")
		data := make([]struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}, len(req.Input))
		for i, txt := range req.Input {
			v := float32(txt[0] - 'a')
			data[i] = struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: []float32{v, v}, Index: i}
		}
		json.NewEncoder(w).Encode(embeddingResponse{Data: data})
	})

	texts := []string{"a", "b", "c", "d", "e"}
	got, err := c.BatchEmbed(texts, 2)
	if err != nil {
		t.Fatalf("BatchEmbed: %v", err)
	}

	// Expect 3 batches: [a b], [c d], [e].
	if len(batches) != 3 {
		t.Fatalf("got %d batches, want 3", len(batches))
	}
	if !reflect.DeepEqual(batches[0], []string{"a", "b"}) {
		t.Errorf("batch 0 = %v, want [a b]", batches[0])
	}
	if !reflect.DeepEqual(batches[2], []string{"e"}) {
		t.Errorf("batch 2 = %v, want [e]", batches[2])
	}

	// Verify reassembly preserves original order across batches.
	want := [][]float32{{0, 0}, {1, 1}, {2, 2}, {3, 3}, {4, 4}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("embeddings = %v, want %v", got, want)
	}
}

func TestBatchEmbedDefaultBatchSize(t *testing.T) {
	var batchSizes []int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req embeddingRequest
		json.NewDecoder(r.Body).Decode(&req)
		batchSizes = append(batchSizes, len(req.Input))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embeddingResponse{})
	})

	// batchSize <= 0 should fall back to the default (6).
	texts := make([]string, 6)
	if _, err := c.BatchEmbed(texts, 0); err != nil {
		t.Fatalf("BatchEmbed: %v", err)
	}
	if len(batchSizes) != 1 || batchSizes[0] != 6 {
		t.Errorf("batch sizes = %v, want single batch of 6", batchSizes)
	}
}

func TestBatchEmbedErrorPropagates(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad"))
	})

	if _, err := c.BatchEmbed([]string{"a", "b"}, 1); err == nil {
		t.Fatal("expected error from failing batch")
	}
}

func TestEmbedDocumentsAppliesDocumentPrefix(t *testing.T) {
	var gotBody embeddingRequest
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embeddingResponse{})
	})
	c.taskPrefix = TaskPrefix{Query: "search_query: ", Document: "search_document: "}

	if _, err := c.EmbedDocuments([]string{"alpha", "beta"}); err != nil {
		t.Fatalf("EmbedDocuments: %v", err)
	}
	want := []string{"search_document: alpha", "search_document: beta"}
	if !reflect.DeepEqual(gotBody.Input, want) {
		t.Errorf("input = %v, want %v", gotBody.Input, want)
	}
}

func TestEmbedQueryAppliesQueryPrefix(t *testing.T) {
	var gotBody embeddingRequest
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embeddingResponse{})
	})
	c.taskPrefix = TaskPrefix{Query: "search_query: ", Document: "search_document: "}

	if _, err := c.EmbedQuery("hello"); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if !reflect.DeepEqual(gotBody.Input, []string{"search_query: hello"}) {
		t.Errorf("input = %v, want [search_query: hello]", gotBody.Input)
	}
}

func TestTaskPrefixZeroValueSendsRawText(t *testing.T) {
	var inputs [][]string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body embeddingRequest
		json.NewDecoder(r.Body).Decode(&body)
		inputs = append(inputs, body.Input)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embeddingResponse{})
	})

	if _, err := c.EmbedDocuments([]string{"plain"}); err != nil {
		t.Fatalf("EmbedDocuments: %v", err)
	}
	if _, err := c.EmbedQuery("plain query"); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}

	if len(inputs) != 2 {
		t.Fatalf("captured %d requests, want 2", len(inputs))
	}
	if !reflect.DeepEqual(inputs[0], []string{"plain"}) {
		t.Errorf("document input = %v, want [plain]", inputs[0])
	}
	if !reflect.DeepEqual(inputs[1], []string{"plain query"}) {
		t.Errorf("query input = %v, want [plain query]", inputs[1])
	}
}

func TestTaskPrefixIdempotent(t *testing.T) {
	var inputs [][]string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body embeddingRequest
		json.NewDecoder(r.Body).Decode(&body)
		inputs = append(inputs, body.Input)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embeddingResponse{})
	})
	c.taskPrefix = TaskPrefix{Query: "search_query: ", Document: "search_document: "}

	// Pass already prefixed strings
	if _, err := c.EmbedDocuments([]string{"search_document: doc1", "doc2"}); err != nil {
		t.Fatalf("EmbedDocuments: %v", err)
	}
	if _, err := c.EmbedQuery("search_query: query1"); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}

	wantDoc := []string{"search_document: doc1", "search_document: doc2"}
	if !reflect.DeepEqual(inputs[0], wantDoc) {
		t.Errorf("document input = %v, want %v", inputs[0], wantDoc)
	}
	wantQuery := []string{"search_query: query1"}
	if !reflect.DeepEqual(inputs[1], wantQuery) {
		t.Errorf("query input = %v, want %v", inputs[1], wantQuery)
	}
}
