package store

import (
	"context"
	"strings"
	"testing"
)

func TestUpsertAndReplaceIndexIsAtomic(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("writer", CollectionTypeMarkdown, t.TempDir(), "*.md")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	initial := DocumentIndex{
		CollectionID: col.ID,
		Path:         "note.md",
		Title:        "old title",
		ContentHash:  "old",
		Mtime:        1,
		LineCount:    1,
		FTSContent:   "old searchable content",
		Chunks:       []IndexChunk{{Seq: 0, Content: "old searchable content", StartLine: 1, EndLine: 1}},
		FastFields:   map[string]string{"lang": "go", "obsolete": "yes"},
	}
	if _, err := s.UpsertAndReplaceIndex(ctx, initial); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	failed := initial
	failed.Title = "new title"
	failed.FTSContent = "new content"
	failed.Chunks = []IndexChunk{
		{Seq: 0, Content: "new content", StartLine: 1, EndLine: 1},
		{Seq: 1, Content: "this must roll back", ChunkType: ChunkType(99)},
	}
	failed.FastFields = map[string]string{"lang": "rust"}
	if _, err := s.UpsertAndReplaceIndex(ctx, failed); err == nil {
		t.Fatal("expected invalid chunk type to fail")
	}

	oldResults, err := s.SearchFTS("old", 10, nil)
	if err != nil {
		t.Fatalf("search old content: %v", err)
	}
	if len(oldResults) != 1 || oldResults[0].Title != "old title" {
		t.Fatalf("old state was not preserved: %#v", oldResults)
	}
	newResults, err := s.SearchFTS("new", 10, nil)
	if err != nil {
		t.Fatalf("search new content: %v", err)
	}
	if len(newResults) != 0 {
		t.Fatalf("failed write leaked new FTS state: %#v", newResults)
	}

	doc, err := s.GetDocument(col.ID, "note.md")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.FastFields().Get(doc.ID, "obsolete"); err != nil || got != "yes" {
		t.Fatalf("obsolete fast field after rollback = %#v, %v; want yes", got, err)
	}

	success := failed
	success.Chunks = []IndexChunk{{Seq: 0, Content: "new content", StartLine: 1, EndLine: 1}}
	if _, err := s.UpsertAndReplaceIndex(ctx, success); err != nil {
		t.Fatalf("successful replacement: %v", err)
	}
	if got, err := s.FastFields().Get(doc.ID, "obsolete"); err != nil || got != nil {
		t.Fatalf("stale fast field = %#v, %v; want nil", got, err)
	}
	if got, err := s.FastFields().Get(doc.ID, "lang"); err != nil || got != "rust" {
		t.Fatalf("new fast field = %#v, %v; want rust", got, err)
	}
}

func TestUpsertAndReplaceIndexHonorsCanceledContext(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("writer-cancel", CollectionTypeMarkdown, t.TempDir(), "*.md")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.UpsertAndReplaceIndex(ctx, DocumentIndex{CollectionID: col.ID, Path: "cancel.md"})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}
