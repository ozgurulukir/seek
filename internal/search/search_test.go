package search

import (
	"math"
	"testing"

	"context"
	"encoding/json"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/store"
	"net/http"
	"net/http/httptest"
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

func TestEngine_SearchVector(t *testing.T) {
	mux := http.NewServeMux()

	// Mock handler for embed.Client
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

	// Mock handler for embed.VLClient
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
	defer ts.Close()

	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	t.Run("no embed clients", func(t *testing.T) {
		engine := NewEngine(s, nil)
		_, err := engine.SearchVector(context.Background(), "query", 10, Options{})
		if err == nil {
			t.Fatal("expected error when no embed client is provided")
		}
		if err.Error() != "vector search requires embedding client" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("with embedClient", func(t *testing.T) {
		client := embed.NewClient(ts.URL, "key", "model", 3)
		engine := NewEngine(s, client)

		results, err := engine.SearchVector(context.Background(), "query", 0, Options{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Note: since the store is empty, results will be empty.
		// We are mainly asserting that the mock was called properly and no error returned.
		if results == nil {
			t.Fatal("expected non-nil (or empty slice) results") // store.SearchVector returns empty slice or nil on no results.
		}
	})

	t.Run("with vlClient precedence", func(t *testing.T) {
		// Create a broken embed client that points to a non-existent URL.
		// If vlClient takes precedence, this broken client won't be called.
		badClient := embed.NewClient("http://127.0.0.1:0", "key", "model", 3)
		vlClient := embed.NewVLClient("key", "model", 3, ts.URL+"/vl-embeddings")

		engine := NewEngineWithVL(s, badClient, vlClient)

		results, err := engine.SearchVector(context.Background(), "query", 5, Options{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// If it reaches here, it means vlClient was successfully called.
		_ = results
	})

	t.Run("limit defaults to DefaultLimit", func(t *testing.T) {
		client := embed.NewClient(ts.URL, "key", "model", 3)
		engine := NewEngine(s, client)

		// If limit is <= 0, it becomes DefaultLimit inside SearchVector.
		// The mock store handles limit, but since the store is empty,
		// we won't see length=20. We can just test it doesn't fail.
		_, err := engine.SearchVector(context.Background(), "query", -5, Options{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("embedClient returns error", func(t *testing.T) {
		// client points to a broken URL that returns a non-200 status or fails
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
	})

	t.Run("vlClient returns error", func(t *testing.T) {
		// client points to a broken URL that returns a non-200 status or fails
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
	})
}
