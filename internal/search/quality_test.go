package search

import (
	"context"
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
	if got := ContentKind(Result{}); got != "snippet" {
		t.Errorf("document-level = %q, want snippet", got)
	}
	if got := ContentKind(Result{ChunkID: 7}); got != "full" {
		t.Errorf("chunk-level = %q, want full", got)
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
