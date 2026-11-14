package search

import (
	"context"
	"errors"
	"github.com/ozgurulukir/seek/internal/embed"
	"math"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
)

func TestRRFFusionWithK_PrefersFullChunkOverSnippet(t *testing.T) {
	// A document shared between the BM25 (snippet, ChunkID == 0) and vector
	// (full chunk, ChunkID > 0) legs must keep the richer vector content while
	// retaining the summed RRF score and rank ordering.
	const (
		sharedDoc = int64(1)
		otherDoc  = int64(2)
	)
	bm25 := []Result{
		{DocumentID: sharedDoc, ChunkID: 0, Content: "short snippet text", Title: "bm25-title"},
		{DocumentID: otherDoc, ChunkID: 0, Content: "other snippet", Title: "other-title"},
	}
	vec := []Result{
		{DocumentID: sharedDoc, ChunkID: 42, Content: "the full chunk content", Title: "vec-title", StartLine: 10, EndLine: 20},
	}

	result := rrfFusionWithK(bm25, vec, DefaultLimit, DefaultRRFK)

	if len(result) != 2 {
		t.Fatalf("expected 2 results (dedup by docID), got %d", len(result))
	}

	// The shared doc scores from both lists → highest → first.
	if result[0].DocumentID != sharedDoc {
		t.Fatalf("expected shared doc %d first, got doc%d", sharedDoc, result[0].DocumentID)
	}

	// The richer vector candidate must win for content + chunk identity.
	got := result[0]
	if kind := ContentKind(got); kind != "full" {
		t.Errorf("ContentKind = %q, want full", kind)
	}
	if got.ChunkID != 42 {
		t.Errorf("ChunkID = %d, want 42", got.ChunkID)
	}
	if got.Content != "the full chunk content" {
		t.Errorf("Content = %q, want full chunk content", got.Content)
	}
	if got.StartLine != 10 || got.EndLine != 20 {
		t.Errorf("line span = (%d,%d), want (10,20)", got.StartLine, got.EndLine)
	}

	// RRF scoring must be unchanged: sum of both rank contributions.
	wantScore := 1.0/float64(DefaultRRFK+1) + 1.0/float64(DefaultRRFK+1)
	if math.Abs(got.Score-wantScore) > 1e-9 {
		t.Errorf("shared doc score = %f, want %f", got.Score, wantScore)
	}

	// The single-source doc keeps its snippet and its lower rank.
	if result[1].DocumentID != otherDoc {
		t.Errorf("expected other doc %d second, got doc%d", otherDoc, result[1].DocumentID)
	}
	if kind := ContentKind(result[1]); kind != "snippet" {
		t.Errorf("other doc ContentKind = %q, want snippet", kind)
	}
	wantOther := 1.0 / float64(DefaultRRFK+2)
	if math.Abs(result[1].Score-wantOther) > 1e-9 {
		t.Errorf("other doc score = %f, want %f", result[1].Score, wantOther)
	}
}

func TestRRFFusionWithK_KeepsSnippetWhenVectorIsNotRicher(t *testing.T) {
	// When both legs are document-level (ChunkID == 0), the first-seen BM25
	// entry must be retained — the fix only upgrades to a richer candidate.
	bm25 := []Result{{DocumentID: 1, ChunkID: 0, Content: "bm25 snippet", Title: "bm25-title"}}
	vec := []Result{{DocumentID: 1, ChunkID: 0, Content: "vec snippet", Title: "vec-title"}}

	result := rrfFusionWithK(bm25, vec, DefaultLimit, DefaultRRFK)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].Content != "bm25 snippet" {
		t.Errorf("expected BM25 snippet retained, got %q", result[0].Content)
	}
}

func TestRRFFusionWithK_MultipleVectorChunksKeepsHighestRanked(t *testing.T) {
	// When multiple vector chunks match the same document, the highest-ranking
	// vector chunk should be preserved as the representative chunk.
	bm25 := []Result{{DocumentID: 1, ChunkID: 0, Content: "bm25 snippet"}}
	vec := []Result{
		{DocumentID: 1, ChunkID: 10, Content: "vector chunk 10 (top rank)"},
		{DocumentID: 1, ChunkID: 20, Content: "vector chunk 20 (lower rank)"},
	}

	result := rrfFusionWithK(bm25, vec, DefaultLimit, DefaultRRFK)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].ChunkID != 10 {
		t.Errorf("expected ChunkID = 10, got %d", result[0].ChunkID)
	}
	if result[0].Content != "vector chunk 10 (top rank)" {
		t.Errorf("expected content from top-ranked chunk, got %q", result[0].Content)
	}
}

func TestContentKind(t *testing.T) {
	tests := []struct {
		name     string
		result   Result
		wantKind string
	}{
		{
			name:     "zero chunk id returns snippet",
			result:   Result{ChunkID: 0, DocumentID: 1, Title: "doc"},
			wantKind: "snippet",
		},
		{
			name:     "positive chunk id returns full",
			result:   Result{ChunkID: 7, DocumentID: 1, Title: "chunk"},
			wantKind: "full",
		},
		{
			name:     "negative chunk id returns snippet",
			result:   Result{ChunkID: -1, DocumentID: 1, Title: "invalid"},
			wantKind: "snippet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContentKind(tt.result); got != tt.wantKind {
				t.Errorf("ContentKind(%+v) = %q, want %q", tt.result, got, tt.wantKind)
			}
		})
	}
}

func TestEnrichContent_StripsMarkersOnDocumentLevel(t *testing.T) {
	results := []Result{
		{ChunkID: 0, Content: "hello >>>world<<< here"},
		{ChunkID: 0, Content: "no markers"},
	}
	var engine *Engine
	engine.EnrichContent(context.Background(), results)
	if results[0].Content != "hello world here" {
		t.Errorf("markers not stripped: %q", results[0].Content)
	}
	if results[1].Content != "no markers" {
		t.Errorf("unexpected change: %q", results[1].Content)
	}
}

func TestEnrichContent_FullChunkWithNilDB(t *testing.T) {
	// A nil db must leave chunk-level content untouched (guarded).
	results := []Result{{ChunkID: 9, Content: "chunk text"}}
	var engine *Engine
	engine.EnrichContent(context.Background(), results)
	if results[0].Content != "chunk text" {
		t.Errorf("nil db must not touch chunk content, got %q", results[0].Content)
	}
}

func TestEnrichContent_FullChunkFromDB(t *testing.T) {
	// Regression against the marker logic: with a real db, a chunk-level hit
	// gets its full stored content even if it would "look like" a marker.
	s := newTestStore(t)
	col, err := s.CreateCollection("notes", store.CollectionTypeMarkdown, "/tmp", "*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := s.UpsertDocument(col.ID, "/tmp/n.md", "Note", "h", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertChunk(docID, 0, "full >>>content<<< stored", nil); err != nil {
		t.Fatal(err)
	}
	chunks, err := s.GetChunksWithoutEmbedding(false)
	if err != nil || len(chunks) == 0 {
		t.Fatalf("get chunk: %v (%d)", err, len(chunks))
	}
	results := []Result{{ChunkID: chunks[0].ID, Content: ">>>snippet<<<"}}
	engine := NewEngine(NewStoreRepository(s), nil)
	engine.EnrichContent(context.Background(), results)
	if results[0].Content != "full >>>content<<< stored" {
		t.Errorf("full content not fetched, got %q", results[0].Content)
	}
	if !strings.Contains(results[0].Content, ">>>") {
		t.Errorf("marker-strip must not apply to full content: %q", results[0].Content)
	}
}

func TestRRFFusion_UsesDefaultK(t *testing.T) {
	bm25 := []Result{{DocumentID: 1, ChunkID: 0, Content: "bm25 snippet"}}
	vec := []Result{{DocumentID: 2, ChunkID: 10, Content: "vec chunk"}}

	resDefault := rrfFusion(bm25, vec, 10)
	resWithK := rrfFusionWithK(bm25, vec, 10, DefaultRRFK)

	if len(resDefault) != len(resWithK) {
		t.Fatalf("length mismatch: got %d, want %d", len(resDefault), len(resWithK))
	}
	for i := range resDefault {
		if resDefault[i].DocumentID != resWithK[i].DocumentID {
			t.Errorf("[%d] DocumentID = %d, want %d", i, resDefault[i].DocumentID, resWithK[i].DocumentID)
		}
		if resDefault[i].Score != resWithK[i].Score {
			t.Errorf("[%d] Score = %f, want %f", i, resDefault[i].Score, resWithK[i].Score)
		}
	}
}

type mockQualityReranker struct {
	err     error
	results []embed.RerankResult
}

func (m *mockQualityReranker) Rerank(ctx context.Context, query string, documents []string, topN int) ([]embed.RerankResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

func TestRerankResults_NilReranker(t *testing.T) {
	var engine Engine
	ctx := context.Background()

	candidates := []Result{
		{DocumentID: 1, Title: "doc1"},
		{DocumentID: 2, Title: "doc2"},
		{DocumentID: 3, Title: "doc3"},
	}

	got, err := engine.rerankResults(ctx, "test", candidates, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (truncated to limit)", len(got))
	}

	gotNoTrunc, err := engine.rerankResults(ctx, "test", candidates, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotNoTrunc) != 3 {
		t.Fatalf("len(gotNoTrunc) = %d, want 3", len(gotNoTrunc))
	}
}

func TestRerankResults_SingleResultBypass(t *testing.T) {
	mock := &mockQualityReranker{results: []embed.RerankResult{{Index: 0, RelevanceScore: 0.99}}}
	engine := NewEngine(nil, nil).WithReranker(mock)
	ctx := context.Background()

	candidates := []Result{{DocumentID: 1, Title: "single"}}
	got, err := engine.rerankResults(ctx, "test", candidates, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Score != 0 {
		t.Errorf("expected original score preserved when <= 1 candidate, got %f", got[0].Score)
	}
}

func TestRerankResults_ErrorPropagation(t *testing.T) {
	mock := &mockQualityReranker{err: errors.New("reranker service unavailable")}
	engine := NewEngine(nil, nil).WithReranker(mock)
	ctx := context.Background()

	candidates := []Result{
		{DocumentID: 1, Title: "doc1"},
		{DocumentID: 2, Title: "doc2"},
	}

	_, err := engine.rerankResults(ctx, "test", candidates, 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "reranker service unavailable") {
		t.Errorf("error = %v, want contained original error", err)
	}
}

func TestRerankResults_EmptyResponseAndOutOfBoundsIndices(t *testing.T) {
	ctx := context.Background()
	candidates := []Result{
		{DocumentID: 1, Title: "doc1"},
		{DocumentID: 2, Title: "doc2"},
		{DocumentID: 3, Title: "doc3"},
	}

	// Case 1: Empty scores response falls back to limit-truncated candidates
	mockEmpty := &mockQualityReranker{results: []embed.RerankResult{}}
	engineEmpty := NewEngine(nil, nil).WithReranker(mockEmpty)
	gotEmpty, err := engineEmpty.rerankResults(ctx, "test", candidates, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotEmpty) != 2 {
		t.Fatalf("len(gotEmpty) = %d, want 2", len(gotEmpty))
	}

	// Case 2: Out of bounds indices are filtered out
	mockOOB := &mockQualityReranker{
		results: []embed.RerankResult{
			{Index: -1, RelevanceScore: 0.9},
			{Index: 1, RelevanceScore: 0.8},
			{Index: 99, RelevanceScore: 0.7},
		},
	}
	engineOOB := NewEngine(nil, nil).WithReranker(mockOOB)
	gotOOB, err := engineOOB.rerankResults(ctx, "test", candidates, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotOOB) != 1 {
		t.Fatalf("len(gotOOB) = %d, want 1 valid result", len(gotOOB))
	}
	if gotOOB[0].DocumentID != 2 || gotOOB[0].Score != 0.8 {
		t.Errorf("got result %+v, want DocumentID=2 with score 0.8", gotOOB[0])
	}
}
