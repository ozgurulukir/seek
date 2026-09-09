package search

import (
	"context"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
)

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
