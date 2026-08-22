package search

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/store"
)

func mkResult(docID int64, title string) store.SearchResult {
	return store.SearchResult{DocumentID: docID, Title: title}
}

func TestRRFFusionEmptyInputs(t *testing.T) {
	result := rrfFusion(nil, nil, DefaultLimit)
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

func TestRRFFusionSingleSource(t *testing.T) {
	bm25 := []store.SearchResult{
		mkResult(1, "doc1"),
		mkResult(2, "doc2"),
	}
	result := rrfFusion(bm25, nil, DefaultLimit)

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	// BM25-only results should be in rank order (doc1 first, doc2 second).
	if result[0].DocumentID != 1 {
		t.Errorf("expected doc1 first, got doc%d", result[0].DocumentID)
	}
	if result[1].DocumentID != 2 {
		t.Errorf("expected doc2 second, got doc%d", result[1].DocumentID)
	}
}

func TestRRFFusionVectorOnly(t *testing.T) {
	vec := []store.SearchResult{
		mkResult(10, "doc10"),
		mkResult(20, "doc20"),
	}
	result := rrfFusion(nil, vec, DefaultLimit)

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].DocumentID != 10 {
		t.Errorf("expected doc10 first, got doc%d", result[0].DocumentID)
	}
	if result[1].DocumentID != 20 {
		t.Errorf("expected doc20 second, got doc%d", result[1].DocumentID)
	}
}

func TestRRFFusionOverlapBoost(t *testing.T) {
	// doc1 appears in both BM25 (rank 0) and vector (rank 0).
	// doc2 appears only in BM25 (rank 1).
	bm25 := []store.SearchResult{
		mkResult(1, "doc1"),
		mkResult(2, "doc2"),
	}
	vec := []store.SearchResult{
		mkResult(1, "doc1"),
		mkResult(3, "doc3"),
	}
	result := rrfFusion(bm25, vec, DefaultLimit)

	if len(result) != 3 {
		t.Fatalf("expected 3 results (dedup by docID), got %d", len(result))
	}

	// doc1 appears in both lists → highest RRF score → first.
	if result[0].DocumentID != 1 {
		t.Errorf("expected doc1 (in both lists) first, got doc%d", result[0].DocumentID)
	}

	// Verify doc1's score is the sum of both contributions.
	want := 1.0/float64(RRFk+1) + 1.0/float64(RRFk+1)
	got := result[0].Score
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("expected doc1 score=%f, got %f", want, got)
	}
}

func TestRRFFusionDedupByDocumentID(t *testing.T) {
	// Same document in both lists should appear only once.
	bm25 := []store.SearchResult{
		mkResult(1, "bm25-version"),
	}
	vec := []store.SearchResult{
		mkResult(1, "vec-version"),
	}
	result := rrfFusion(bm25, vec, DefaultLimit)

	if len(result) != 1 {
		t.Fatalf("expected 1 result after dedup, got %d", len(result))
	}
	// The result should keep the BM25 version (first seen).
	if result[0].Title != "bm25-version" {
		t.Errorf("expected BM25 version to win, got %q", result[0].Title)
	}
	// But the score should include both contributions.
	want := 1.0/float64(RRFk+1) + 1.0/float64(RRFk+1)
	if math.Abs(result[0].Score-want) > 1e-9 {
		t.Errorf("expected combined score=%f, got %f", want, result[0].Score)
	}
}

func TestRRFFusionLimit(t *testing.T) {
	bm25 := []store.SearchResult{
		mkResult(1, "doc1"),
		mkResult(2, "doc2"),
		mkResult(3, "doc3"),
	}
	vec := []store.SearchResult{
		mkResult(4, "doc4"),
		mkResult(5, "doc5"),
	}
	result := rrfFusion(bm25, vec, 3)

	if len(result) != 3 {
		t.Fatalf("expected 3 results (limited), got %d", len(result))
	}
}

func TestRRFFusionLimitZero(t *testing.T) {
	// limit=0 should return all results (or no results — either is fine, no panic).
	bm25 := []store.SearchResult{
		mkResult(1, "doc1"),
		mkResult(2, "doc2"),
	}
	// No panic expected.
	_ = rrfFusion(bm25, nil, 0)
}

func TestRRFFusionScoringFormula(t *testing.T) {
	// Verify the RRF formula for single-source ordering.
	// rank 0 → 1/61, rank 1 → 1/62, etc.
	bm25 := []store.SearchResult{
		mkResult(1, "doc1"), // rank 0 → 1/61
		mkResult(2, "doc2"), // rank 1 → 1/62
	}
	result := rrfFusion(bm25, nil, DefaultLimit)

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	wantDoc1 := 1.0 / float64(RRFk+1) // 1/61
	wantDoc2 := 1.0 / float64(RRFk+2) // 1/62
	if math.Abs(result[0].Score-wantDoc1) > 1e-9 {
		t.Errorf("doc1 score=%f, want %f", result[0].Score, wantDoc1)
	}
	if math.Abs(result[1].Score-wantDoc2) > 1e-9 {
		t.Errorf("doc2 score=%f, want %f", result[1].Score, wantDoc2)
	}
}

func TestRRFFusionDistinctDocsRanking(t *testing.T) {
	// 5 docs in BM25, 2 distinct docs in vector.
	// All docs are unique across lists, so we expect 7 results.
	// Scores: bm25 ranks 0-4 = 1/61..1/65, vec ranks 0-1 = 1/61, 1/62.
	// No ties at positions 0-1 (both 1/61) — but map iteration order makes
	// tie order non-deterministic. So we only verify the result set and
	// that the unique top-scoring doc (doc3, 1/63) appears before doc4 (1/64).
	bm25 := []store.SearchResult{
		mkResult(1, "doc1"),
		mkResult(2, "doc2"),
		mkResult(3, "doc3"),
		mkResult(4, "doc4"),
		mkResult(5, "doc5"),
	}
	vec := []store.SearchResult{
		mkResult(6, "doc6"),
		mkResult(7, "doc7"),
	}
	result := rrfFusion(bm25, vec, DefaultLimit)

	// All 7 unique docs should be present.
	if len(result) != 7 {
		t.Fatalf("expected 7 results, got %d", len(result))
	}

	// Build a map of docID → score for verification.
	scoreMap := make(map[int64]float64)
	for _, r := range result {
		scoreMap[r.DocumentID] = r.Score
	}

	// Verify each doc has the expected score.
	expected := map[int64]float64{
		1: 1.0 / float64(RRFk+1), // BM25 rank 0
		2: 1.0 / float64(RRFk+2), // BM25 rank 1
		3: 1.0 / float64(RRFk+3), // BM25 rank 2
		4: 1.0 / float64(RRFk+4), // BM25 rank 3
		5: 1.0 / float64(RRFk+5), // BM25 rank 4
		6: 1.0 / float64(RRFk+1), // vec rank 0
		7: 1.0 / float64(RRFk+2), // vec rank 1
	}
	for docID, want := range expected {
		got, ok := scoreMap[docID]
		if !ok {
			t.Errorf("doc%d missing from results", docID)
			continue
		}
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("doc%d score=%f, want %f", docID, got, want)
		}
	}

	// Verify result order is non-increasing by score (weakly sorted).
	for i := 1; i < len(result); i++ {
		if result[i].Score > result[i-1].Score {
			t.Errorf("results not sorted: result[%d].Score=%f > result[%d].Score=%f",
				i, result[i].Score, i-1, result[i-1].Score)
		}
	}
}

func TestRRFFusionBM25TakesPrecedenceOnTie(t *testing.T) {
	// Same doc in both lists at same rank. BM25 version should be kept.
	bm25 := []store.SearchResult{
		mkResult(1, "from-bm25"),
	}
	vec := []store.SearchResult{
		mkResult(1, "from-vec"),
		mkResult(2, "vec-only"),
	}
	result := rrfFusion(bm25, vec, DefaultLimit)

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	// doc1 (score 2/61) should be first.
	if result[0].DocumentID != 1 {
		t.Errorf("expected doc1 first, got doc%d", result[0].DocumentID)
	}
	// BM25 metadata should win for the overlapping doc.
	if result[0].Title != "from-bm25" {
		t.Errorf("expected BM25 title, got %q", result[0].Title)
	}
}

// --- shared helpers (deduplicated from PRs #24, #29, #34) ---

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newTestEmbedClient(t *testing.T, handler http.HandlerFunc) *embed.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return embed.NewClient(srv.URL, "test-key", "test-model", 2)
}

func insertDoc(t *testing.T, s *store.Store, path, title, content string) int64 {
	t.Helper()
	col, err := s.GetCollectionByName("test-col")
	if err != nil {
		col, err = s.CreateCollection("test-col", "markdown", "/tmp", "**/*.md")
		if err != nil {
			t.Fatalf("CreateCollection: %v", err)
		}
	}
	docID, err := s.UpsertDocument(col.ID, path, title, "hash", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	// Insert dummy chunk to populate FTS.
	if err := s.InsertChunk(docID, 0, content, nil); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}
	if err := s.UpsertFTS(docID, title, content); err != nil {
		t.Fatalf("UpsertFTS: %v", err)
	}
	s.FastFields().Set(docID, "title", title)
	return docID
}

// --- TestNewEngine / TestNewEngineWithVL (from PR #2) ---

func TestNewEngine(t *testing.T) {
	// We can use nil pointers since NewEngine simply assigns them.
	e := NewEngine(nil, nil)
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.store != nil {
		t.Errorf("expected store to be nil, got %v", e.store)
	}
	if e.embedClient != nil {
		t.Errorf("expected embedClient to be nil, got %v", e.embedClient)
	}
	if e.vlClient != nil {
		t.Errorf("expected vlClient to be nil, got %v", e.vlClient)
	}
}

func TestNewEngineWithVL(t *testing.T) {
	// We can use nil pointers since NewEngineWithVL simply assigns them.
	e := NewEngineWithVL(nil, nil, nil)
	if e == nil {
		t.Fatal("NewEngineWithVL returned nil")
	}
	if e.store != nil {
		t.Errorf("expected store to be nil, got %v", e.store)
	}
	if e.embedClient != nil {
		t.Errorf("expected embedClient to be nil, got %v", e.embedClient)
	}
	if e.vlClient != nil {
		t.Errorf("expected vlClient to be nil, got %v", e.vlClient)
	}
}

// --- TestRunAggregations (from PR #24) ---

func TestRunAggregations(t *testing.T) {
	s := newTestStore(t)
	engine := NewEngine(s, nil)
	ctx := context.Background()

	// 1. Setup collections and documents.
	colMD, err := s.CreateCollection("col-md", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection markdown: %v", err)
	}
	colCode, err := s.CreateCollection("col-code", "code", "/tmp", "**/*.go")
	if err != nil {
		t.Fatalf("CreateCollection code: %v", err)
	}

	// Docs for markdown (type will be markdown).
	_, _ = s.UpsertDocument(colMD.ID, "/tmp/doc1.md", "doc1", "hash1", 1, 5)
	_, _ = s.UpsertDocument(colMD.ID, "/tmp/doc2.md", "doc2", "hash2", 1, 15)

	// Docs for code (type will be code).
	_, _ = s.UpsertDocument(colCode.ID, "/tmp/doc3.go", "doc3", "hash3", 1, 50)
	_, _ = s.UpsertDocument(colCode.ID, "/tmp/doc4.go", "doc4", "hash4", 1, 150)
	_, _ = s.UpsertDocument(colCode.ID, "/tmp/doc5.go", "doc5", "hash5", 1, 20)

	// 2. Test successful aggregations.
	specs := []string{
		"type:terms",
		"line_count:range:0-10,10-100",
		"count",
	}

	result, err := engine.RunAggregations(ctx, specs, nil)
	if err != nil {
		t.Fatalf("RunAggregations failed: %v", err)
	}

	// Verify type:terms.
	typeTerms, ok := result["type:terms"]
	if !ok {
		t.Fatalf("missing type:terms in results")
	}

	termCounts := make(map[string]int)
	for _, b := range typeTerms {
		termCounts[b.Key] = b.Count
	}

	if termCounts["code"] != 3 {
		t.Errorf("expected 3 code documents, got %d", termCounts["code"])
	}
	if termCounts["markdown"] != 2 {
		t.Errorf("expected 2 markdown documents, got %d", termCounts["markdown"])
	}

	// Verify line_count:range.
	lineRanges, ok := result["line_count:range:0-10,10-100"]
	if !ok {
		t.Fatalf("missing line_count:range in results")
	}

	rangeCounts := make(map[string]int)
	for _, b := range lineRanges {
		rangeCounts[b.Key] = b.Count
	}

	if rangeCounts["0-10"] != 1 { // doc1(5)
		t.Errorf("expected 1 document in 0-10, got %d", rangeCounts["0-10"])
	}
	if rangeCounts["10-100"] != 3 { // doc2(15), doc3(50), doc5(20)
		t.Errorf("expected 3 documents in 10-100, got %d", rangeCounts["10-100"])
	}
	if rangeCounts["other"] != 1 { // doc4(150)
		t.Errorf("expected 1 document in other, got %d", rangeCounts["other"])
	}

	// Verify count.
	countRes, ok := result["count"]
	if !ok {
		t.Fatalf("missing count in results")
	}
	if len(countRes) != 1 || countRes[0].Count != 5 {
		t.Errorf("expected total count 5, got %v", countRes)
	}

	// 3. Test parsing error.
	_, err = engine.RunAggregations(ctx, []string{"invalid_spec_format"}, nil)
	if err == nil {
		t.Errorf("expected error for invalid spec, got nil")
	}

	// 4. Test execution error.
	_, err = engine.RunAggregations(ctx, []string{"invalid_column_name:terms"}, nil)
	if err == nil {
		t.Errorf("expected error for invalid column execution, got nil")
	}
}

// --- TestSearchHybrid* (from PR #29) ---
// Note: the original PR called NewVLClient with the wrong argument order
// (srv.URL as apiKey). Fixed here to match the real signature
// (apiKey, model, dimensions, endpoint).

func TestSearchHybridBasic(t *testing.T) {
	s := newTestStore(t)

	col, _ := s.CreateCollection("test", "markdown", "/tmp", "*.md")
	doc1, _ := s.UpsertDocument(col.ID, "/tmp/doc1.md", "apple doc", "hash1", 1, 1)
	doc2, _ := s.UpsertDocument(col.ID, "/tmp/doc2.md", "grape doc", "hash2", 1, 1)

	_ = s.InsertChunk(doc1, 0, "apple banana", []float32{1.0, 0.0})
	_ = s.UpsertFTS(doc1, "apple doc", "apple banana")

	_ = s.InsertChunk(doc2, 0, "grape orange", []float32{0.0, 1.0})
	_ = s.UpsertFTS(doc2, "grape doc", "grape orange")

	embedClient := newTestEmbedClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"embedding":[1.0, 0.0],"index":0}]}`))
	})

	engine := NewEngine(s, embedClient)

	ctx := context.Background()
	results, err := engine.SearchHybrid(ctx, "apple", 10, Options{})
	if err != nil {
		t.Fatalf("SearchHybrid error: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected results, got 0")
	}
	if results[0].DocumentID != doc1 {
		t.Errorf("expected doc1, got %d", results[0].DocumentID)
	}
}

func TestSearchHybridFallback(t *testing.T) {
	s := newTestStore(t)

	col, _ := s.CreateCollection("test", "markdown", "/tmp", "*.md")
	doc1, _ := s.UpsertDocument(col.ID, "/tmp/doc1.md", "apple doc", "hash1", 1, 1)
	doc2, _ := s.UpsertDocument(col.ID, "/tmp/doc2.md", "apple second", "hash2", 1, 1)

	_ = s.InsertChunk(doc1, 0, "apple banana", []float32{1.0, 0.0})
	_ = s.UpsertFTS(doc1, "apple doc", "apple banana")

	_ = s.InsertChunk(doc2, 0, "apple grape", []float32{0.0, 1.0})
	_ = s.UpsertFTS(doc2, "apple second", "apple grape")

	// Embedding server returns error to force fallback to BM25.
	embedClient := newTestEmbedClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`"Internal Server Error"`))
	})

	engine := NewEngine(s, embedClient)

	ctx := context.Background()
	results, err := engine.SearchHybrid(ctx, "apple", 1, Options{})
	if err != nil {
		t.Fatalf("SearchHybrid error: %v", err)
	}

	// Should fallback to BM25 and truncate to limit 1.
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].DocumentID != doc1 && results[0].DocumentID != doc2 {
		t.Errorf("expected doc1 or doc2, got %d", results[0].DocumentID)
	}
}

func TestSearchHybridZeroLimit(t *testing.T) {
	s := newTestStore(t)
	col, _ := s.CreateCollection("test", "markdown", "/tmp", "*.md")
	doc1, _ := s.UpsertDocument(col.ID, "/tmp/doc1.md", "apple", "hash1", 1, 1)
	_ = s.InsertChunk(doc1, 0, "apple", []float32{1.0, 0.0})
	_ = s.UpsertFTS(doc1, "apple", "apple")

	embedClient := newTestEmbedClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"embedding":[1.0, 0.0],"index":0}]}`))
	})
	engine := NewEngine(s, embedClient)

	// limit <= 0 should use DefaultLimit.
	results, err := engine.SearchHybrid(context.Background(), "apple", 0, Options{})
	if err != nil {
		t.Fatalf("SearchHybrid error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestSearchHybridVLClient(t *testing.T) {
	s := newTestStore(t)
	col, _ := s.CreateCollection("test", "markdown", "/tmp", "*.md")
	doc1, _ := s.UpsertDocument(col.ID, "/tmp/doc1.md", "apple", "hash1", 1, 1)
	_ = s.InsertChunk(doc1, 0, "apple", []float32{1.0, 0.0})
	_ = s.UpsertFTS(doc1, "apple", "apple")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"output":{"embeddings":[{"embedding":[1.0, 0.0]}]}}`)) // DashScope format.
	}))
	t.Cleanup(srv.Close)

	vlClient := embed.NewVLClient("test-key", "test-model", 2, srv.URL)
	engine := NewEngineWithVL(s, nil, vlClient)

	results, err := engine.SearchHybrid(context.Background(), "apple", 10, Options{})
	if err != nil {
		t.Fatalf("SearchHybrid error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// --- TestEngine_SearchVector (from PR #33) ---

func newMockVectorServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Mock handler for embed.Client.
	mux.HandleFunc("/embeddings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"embedding": []float32{0.1, 0.2, 0.3},
					"index":     0,
				},
			},
		})
	})

	// Mock handler for embed.VLClient.
	mux.HandleFunc("/vl-embeddings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"output": map[string]interface{}{
				"embeddings": []map[string]interface{}{
					{
						"embedding": []float32{0.4, 0.5, 0.6},
						"index":     0,
					},
				},
			},
		})
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func testSearchVectorNoEmbedClients(t *testing.T, s *store.Store) {
	t.Helper()
	engine := NewEngine(s, nil)
	_, err := engine.SearchVector(context.Background(), "query", 10, Options{})
	if err == nil {
		t.Fatal("expected error when no embed client is provided")
	}
	if err.Error() != "vector search requires embedding client" {
		t.Errorf("unexpected error: %v", err)
	}
}

func testSearchVectorWithEmbedClient(t *testing.T, s *store.Store, baseURL string) {
	t.Helper()
	client := embed.NewClient(baseURL, "key", "model", 3)
	engine := NewEngine(s, client)

	results, err := engine.SearchVector(context.Background(), "query", 0, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Store is empty; results may be nil or an empty slice — both are acceptable here.
	_ = results
}

func testSearchVectorWithVLClientPrecedence(t *testing.T, s *store.Store, baseURL string) {
	t.Helper()
	// Create a broken embed client that points to a non-existent URL.
	// If vlClient takes precedence, this broken client won't be called.
	badClient := embed.NewClient("http://127.0.0.1:0", "key", "model", 3)
	vlClient := embed.NewVLClient("key", "model", 3, baseURL+"/vl-embeddings")

	engine := NewEngineWithVL(s, badClient, vlClient)

	results, err := engine.SearchVector(context.Background(), "query", 5, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// If it reaches here, it means vlClient was successfully called.
	_ = results
}

func testSearchVectorLimitDefaults(t *testing.T, s *store.Store, baseURL string) {
	t.Helper()
	client := embed.NewClient(baseURL, "key", "model", 3)
	engine := NewEngine(s, client)

	// If limit is <= 0, it becomes DefaultLimit inside SearchVector.
	_, err := engine.SearchVector(context.Background(), "query", -5, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testSearchVectorEmbedClientError(t *testing.T, s *store.Store) {
	t.Helper()
	brokenMux := http.NewServeMux()
	brokenMux.HandleFunc("/embeddings", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	brokenTs := httptest.NewServer(brokenMux)
	defer brokenTs.Close()

	client := embed.NewClient(brokenTs.URL, "key", "model", 3)
	engine := NewEngine(s, client)

	_, err := engine.SearchVector(context.Background(), "query", 0, Options{})
	if err == nil {
		t.Fatal("expected error from bad embed client")
	}
}

func testSearchVectorVLClientError(t *testing.T, s *store.Store) {
	t.Helper()
	brokenMux := http.NewServeMux()
	brokenMux.HandleFunc("/vl-embeddings", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	brokenTs := httptest.NewServer(brokenMux)
	defer brokenTs.Close()

	badClient := embed.NewClient("http://127.0.0.1:0", "key", "model", 3)
	vlClient := embed.NewVLClient("key", "model", 3, brokenTs.URL+"/vl-embeddings")
	engine := NewEngineWithVL(s, badClient, vlClient)

	_, err := engine.SearchVector(context.Background(), "query", 0, Options{})
	if err == nil {
		t.Fatal("expected error from bad vl client")
	}
}

func TestEngine_SearchVector(t *testing.T) {
	ts := newMockVectorServer(t)
	s := newTestStore(t)

	t.Run("no embed clients", func(t *testing.T) {
		testSearchVectorNoEmbedClients(t, s)
	})

	t.Run("with embedClient", func(t *testing.T) {
		testSearchVectorWithEmbedClient(t, s, ts.URL)
	})

	t.Run("with vlClient precedence", func(t *testing.T) {
		testSearchVectorWithVLClientPrecedence(t, s, ts.URL)
	})

	t.Run("limit defaults to DefaultLimit", func(t *testing.T) {
		testSearchVectorLimitDefaults(t, s, ts.URL)
	})

	t.Run("embedClient returns error", func(t *testing.T) {
		testSearchVectorEmbedClientError(t, s)
	})

	t.Run("vlClient returns error", func(t *testing.T) {
		testSearchVectorVLClientError(t, s)
	})
}

// --- TestSearchBM25 (from PR #34) ---
// Note: two sub-tests in the original PR had empty if-bodies ("with parsed
// query", "with query mode parsed") that never asserted. Strengthened here to
// actually check the expected hit counts.

func TestSearchBM25(t *testing.T) {
	s := newTestStore(t)
	insertDoc(t, s, "/docs/1", "Go Language", "Go is an open source programming language that makes it easy to build simple, reliable, and efficient software.")
	insertDoc(t, s, "/docs/2", "Python", "Python is a programming language that lets you work quickly and integrate systems more effectively.")
	insertDoc(t, s, "/docs/3", "Rust Language", "Rust is a language empowering everyone to build reliable and efficient software.")

	engine := NewEngine(s, nil)
	ctx := context.Background()

	t.Run("basic search", func(t *testing.T) {
		res, err := engine.SearchBM25(ctx, "Go", 10, Options{})
		if err != nil {
			t.Fatalf("SearchBM25 error: %v", err)
		}
		if len(res) == 0 {
			t.Fatalf("expected results, got none")
		}
		if res[0].Title != "Go Language" {
			t.Errorf("expected 'Go Language', got '%s'", res[0].Title)
		}
	})

	t.Run("fallback limit", func(t *testing.T) {
		res, err := engine.SearchBM25(ctx, "language", 0, Options{}) // limit 0 should fall back to DefaultLimit.
		if err != nil {
			t.Fatalf("SearchBM25 error: %v", err)
		}
		if len(res) != 3 {
			t.Errorf("expected 3 results for 'language', got %d", len(res))
		}
	})

	t.Run("with parsed query", func(t *testing.T) {
		query, err := ParseQuery("Go AND software")
		if err != nil {
			t.Fatalf("ParseQuery error: %v", err)
		}
		res, err := engine.SearchBM25(ctx, "Go AND software", 10, Options{Query: query, QueryMode: "parsed"})
		if err != nil {
			t.Fatalf("SearchBM25 error: %v", err)
		}
		// "Go AND software" matches only the Go document.
		if len(res) != 1 {
			t.Errorf("expected 1 result for 'Go AND software', got %d", len(res))
		}
	})

	t.Run("with query mode parsed", func(t *testing.T) {
		res, err := engine.SearchBM25(ctx, "reliable AND software", 10, Options{QueryMode: "parsed"})
		if err != nil {
			t.Fatalf("SearchBM25 error: %v", err)
		}
		// 'reliable AND software' is present in Go and Rust.
		if len(res) != 2 {
			t.Errorf("expected 2 results for 'reliable AND software', got %d", len(res))
		}
	})

	t.Run("with filters", func(t *testing.T) {
		// Only get the Python doc.
		filters := store.NewFilterSet()
		filters.Add(&store.PathFilter{Pattern: "/docs/2"})
		res, err := engine.SearchBM25(ctx, "programming", 10, Options{Filters: filters})
		if err != nil {
			t.Fatalf("SearchBM25 error: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("expected 1 result, got %d", len(res))
		}
		if res[0].Title != "Python" {
			t.Errorf("expected 'Python', got '%s'", res[0].Title)
		}
	})

	t.Run("with sorting (title asc)", func(t *testing.T) {
		res, err := engine.SearchBM25(ctx, "language", 10, Options{SortBy: "title", SortOrder: "asc"})
		if err != nil {
			t.Fatalf("SearchBM25 error: %v", err)
		}
		// Title sort: Go Language, Python, Rust Language.
		if len(res) != 3 {
			t.Fatalf("expected 3 results, got %d", len(res))
		}
		if res[0].Title != "Go Language" || res[1].Title != "Python" || res[2].Title != "Rust Language" {
			t.Errorf("unexpected sort order: %s, %s, %s", res[0].Title, res[1].Title, res[2].Title)
		}
	})

	t.Run("with sorting (title desc)", func(t *testing.T) {
		res, err := engine.SearchBM25(ctx, "language", 10, Options{SortBy: "title", SortOrder: "desc"})
		if err != nil {
			t.Fatalf("SearchBM25 error: %v", err)
		}
		// Title sort desc: Rust Language, Python, Go Language.
		if len(res) != 3 {
			t.Fatalf("expected 3 results, got %d", len(res))
		}
		if res[0].Title != "Rust Language" || res[1].Title != "Python" || res[2].Title != "Go Language" {
			t.Errorf("unexpected sort order: %s, %s, %s", res[0].Title, res[1].Title, res[2].Title)
		}
	})
}
