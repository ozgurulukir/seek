package search

import (
	"context"
	"math"
	"path/filepath"
	"testing"

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

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRunAggregations(t *testing.T) {
	s := newTestStore(t)
	engine := NewEngine(s, nil)
	ctx := context.Background()

	// 1. Setup collections and documents
	colMD, err := s.CreateCollection("col-md", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection markdown: %v", err)
	}
	colCode, err := s.CreateCollection("col-code", "code", "/tmp", "**/*.go")
	if err != nil {
		t.Fatalf("CreateCollection code: %v", err)
	}

	// Docs for markdown (type will be markdown)
	_, _ = s.UpsertDocument(colMD.ID, "/tmp/doc1.md", "doc1", "hash1", 1, 5)
	_, _ = s.UpsertDocument(colMD.ID, "/tmp/doc2.md", "doc2", "hash2", 1, 15)

	// Docs for code (type will be code)
	_, _ = s.UpsertDocument(colCode.ID, "/tmp/doc3.go", "doc3", "hash3", 1, 50)
	_, _ = s.UpsertDocument(colCode.ID, "/tmp/doc4.go", "doc4", "hash4", 1, 150)
	_, _ = s.UpsertDocument(colCode.ID, "/tmp/doc5.go", "doc5", "hash5", 1, 20)

	// 2. Test successful aggregations
	specs := []string{
		"type:terms",
		"line_count:range:0-10,10-100",
		"count",
	}

	result, err := engine.RunAggregations(ctx, specs, nil)
	if err != nil {
		t.Fatalf("RunAggregations failed: %v", err)
	}

	// Verify type:terms
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

	// Verify line_count:range
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

	// Verify count
	countRes, ok := result["count"]
	if !ok {
		t.Fatalf("missing count in results")
	}
	if len(countRes) != 1 || countRes[0].Count != 5 {
		t.Errorf("expected total count 5, got %v", countRes)
	}

	// 3. Test parsing error
	_, err = engine.RunAggregations(ctx, []string{"invalid_spec_format"}, nil)
	if err == nil {
		t.Errorf("expected error for invalid spec, got nil")
	}

	// 4. Test execution error
	_, err = engine.RunAggregations(ctx, []string{"invalid_column_name:terms"}, nil)
	if err == nil {
		t.Errorf("expected error for invalid column execution, got nil")
	}
}
