package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestRenameCollection verifies the atomic rename only touches the collections
// row: documents/chunks survive, the source path is unchanged, and the old
// name stops resolving while documents still reference the same collection.
func TestRenameCollection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	col, err := s.CreateCollection("old", CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	docID, err := s.UpsertDocument(col.ID, "/tmp/note.md", "Note", "hash", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if err := s.InsertChunk(docID, 0, "content", nil); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}

	if err := s.RenameCollection(ctx, "old", "new"); err != nil {
		t.Fatalf("RenameCollection: %v", err)
	}

	renamed, err := s.GetCollectionByName("new")
	if err != nil {
		t.Fatalf("GetCollectionByName(new): %v", err)
	}
	if renamed.ID != col.ID {
		t.Errorf("renamed id = %d, want %d", renamed.ID, col.ID)
	}
	if renamed.Path != "/tmp" {
		t.Errorf("renamed path = %q, want /tmp (rename must not touch the source path)", renamed.Path)
	}
	if _, err := s.GetCollectionByName("old"); err == nil {
		t.Error("old name still resolves after rename")
	}
	// Document/chunk counts are untouched by the rename.
	docs, _ := s.CountDocuments(col.ID)
	chunks, _ := s.CountChunks(col.ID)
	if docs != 1 || chunks != 1 {
		t.Errorf("counts after rename = %d docs/%d chunks, want 1/1", docs, chunks)
	}
}

func TestRenameCollectionCollision(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateCollection("a", CollectionTypeMarkdown, "/tmp", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCollection("b", CollectionTypeMarkdown, "/tmp", ""); err != nil {
		t.Fatal(err)
	}

	err := s.RenameCollection(ctx, "a", "b")
	if !errors.Is(err, ErrCollectionExists) {
		t.Fatalf("rename collision = %v, want ErrCollectionExists", err)
	}
	// The source collection must be unchanged after the failed rename.
	if _, err := s.GetCollectionByName("a"); err != nil {
		t.Errorf("source collection lost after failed rename: %v", err)
	}
	if _, err := s.GetCollectionByName("b"); err != nil {
		t.Errorf("target collection changed after failed rename: %v", err)
	}
}

func TestRenameCollectionNotFoundAndNoop(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateCollection("a", CollectionTypeMarkdown, "/tmp", ""); err != nil {
		t.Fatal(err)
	}

	if err := s.RenameCollection(ctx, "missing", "b"); err == nil {
		t.Fatal("rename of a missing collection should fail")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want not-found text", err)
	}

	// Renaming a collection to itself is a no-op and keeps the name.
	if err := s.RenameCollection(ctx, "a", "a"); err != nil {
		t.Errorf("rename to self should be a no-op, got: %v", err)
	}
	if _, err := s.GetCollectionByName("a"); err != nil {
		t.Errorf("collection lost after self-rename: %v", err)
	}
}

// TestCollectionDetails verifies the batched detail query returns correct
// per-collection document/chunk/embedded-chunk and semantic-state counts in
// a single query (no N+1).
func TestCollectionDetails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateCollection("empty", CollectionTypeMarkdown, "/tmp", ""); err != nil {
		t.Fatal(err)
	}

	full, err := s.CreateCollection("full", CollectionTypeMarkdown, "/tmp", "")
	if err != nil {
		t.Fatal(err)
	}
	doc1, err := s.UpsertDocument(full.ID, "/tmp/a.md", "a", "h1", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	doc2, err := s.UpsertDocument(full.ID, "/tmp/b.md", "b", "h2", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertChunk(doc1, 0, "a1", []float32{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertChunk(doc1, 1, "a2", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertChunk(doc2, 0, "b1", []float32{3, 4}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSemanticState(ctx, doc1, "fp", SemanticStatusCurrent, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSemanticState(ctx, doc2, "fp", SemanticStatusStale, nil); err != nil {
		t.Fatal(err)
	}

	details, err := s.CollectionDetails(ctx)
	if err != nil {
		t.Fatalf("CollectionDetails: %v", err)
	}
	if len(details) != 2 {
		t.Fatalf("details count = %d, want 2", len(details))
	}

	var fullDetail, emptyDetail *CollectionDetail
	for i := range details {
		switch details[i].Name {
		case "full":
			fullDetail = &details[i]
		case "empty":
			emptyDetail = &details[i]
		}
	}
	if fullDetail == nil || emptyDetail == nil {
		t.Fatalf("details missing: full=%v empty=%v", fullDetail != nil, emptyDetail != nil)
	}
	if fullDetail.Documents != 2 || fullDetail.Chunks != 3 || fullDetail.EmbeddedChunks != 2 {
		t.Errorf("full details = %d docs/%d chunks/%d embedded, want 2/3/2",
			fullDetail.Documents, fullDetail.Chunks, fullDetail.EmbeddedChunks)
	}
	if fullDetail.SemanticCurrent != 1 || fullDetail.SemanticStale != 1 || fullDetail.SemanticError != 0 {
		t.Errorf("semantic counts = %d current/%d stale/%d error, want 1/1/0",
			fullDetail.SemanticCurrent, fullDetail.SemanticStale, fullDetail.SemanticError)
	}
	if emptyDetail.Documents != 0 || emptyDetail.Chunks != 0 || emptyDetail.EmbeddedChunks != 0 {
		t.Errorf("empty details = %d docs/%d chunks/%d embedded, want all zero",
			emptyDetail.Documents, emptyDetail.Chunks, emptyDetail.EmbeddedChunks)
	}
}
