package store

import (
	"context"
	"testing"
)

// TestReplaceIndexFastFields: a replace write with fast fields sets them;
// a subsequent replace write with a nil FastFields map must NOT clear them
// (semantic enrichment is optional — a transient outage must not destroy
// previously stored metadata).
func TestReplaceIndexFastFields(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("c", "pdf", "/p", "*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	path := "/p/doc.pdf"

	// First replace: write tags.
	docID, err := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
		CollectionID: col.ID,
		Path:         path,
		Title:        "doc",
		ContentHash:  "h1",
		FTSContent:   "content",
		FastFields:   map[string]string{"tags": "go,rust", "language": "en"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := s.FastFields().Get(docID, "tags"); err != nil || v != "go,rust" {
		t.Fatalf("tags = %v (%v), want go,rust", v, err)
	}

	// Second replace: content changed, same path, FastFields nil (semantic
	// not computed this pass). Existing fast fields must survive.
	docID2, err := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
		CollectionID: col.ID,
		Path:         path,
		Title:        "doc",
		ContentHash:  "h2",
		FTSContent:   "new content",
		// FastFields deliberately nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if docID2 != docID {
		t.Fatalf("expected same doc id, got %d vs %d", docID2, docID)
	}
	if v, err := s.FastFields().Get(docID, "tags"); err != nil || v != "go,rust" {
		t.Fatalf("tags after nil replace = %v (%v), want preserved go,rust", v, err)
	}
	if v, err := s.FastFields().Get(docID, "language"); err != nil || v != "en" {
		t.Fatalf("language after nil replace = %v (%v), want preserved en", v, err)
	}
}

// TestReplaceIndexFastFieldsExplicitClear: a replace with a non-nil (even
// empty) FastFields map DOES clear stale values, so callers can explicitly
// drop metadata when it was actually recomputed to nothing.
func TestReplaceIndexFastFieldsExplicitClear(t *testing.T) {
	s := newTestStore(t)
	col, _ := s.CreateCollection("c", "pdf", "/p", "*.pdf")
	ctx := context.Background()
	path := "/p/doc.pdf"

	docID, _ := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
		CollectionID: col.ID, Path: path, Title: "d", ContentHash: "h1",
		FTSContent: "x", FastFields: map[string]string{"tags": "go"},
	})
	// Recompute to an empty (non-nil) map: stale "tags" must be cleared.
	docID2, _ := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
		CollectionID: col.ID, Path: path, Title: "d", ContentHash: "h2",
		FTSContent: "y", FastFields: map[string]string{},
	})
	if docID2 != docID {
		t.Fatalf("same path expected same id, got %d vs %d", docID2, docID)
	}
	if v, _ := s.FastFields().Get(docID, "tags"); v != nil {
		t.Fatalf("expected tags cleared with explicit empty map, got %v", v)
	}
}
