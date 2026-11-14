package store

import (
	"testing"
)

// TestDeleteCollectionRemovesEverything exercises the rm success path across
// every per-collection table: documents, chunks, FTS entries, fast fields,
// and the collection row itself must all be gone. The fast_fields and
// documents_fts tables are not covered by any foreign-key cascade, so they
// are exactly what the delete transaction must reach
// (review 2026-09-17 M3).
func TestDeleteCollectionRemovesEverything(t *testing.T) {
	s := newTestStore(t)

	col, err := s.CreateCollection("doomed", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	docID, err := s.UpsertDocument(col.ID, "/tmp/doomed.md", "Doomed", "hash", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if err := s.InsertChunk(docID, 0, "searchable doomed content", nil); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}
	if err := s.UpsertFTS(docID, "Doomed", "searchable doomed content"); err != nil {
		t.Fatalf("UpsertFTS: %v", err)
	}
	if err := s.FastFields().Set(docID, "language", "en"); err != nil {
		t.Fatalf("FastFields.Set: %v", err)
	}

	if err := s.DeleteCollection(col.ID); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}

	for name, query := range map[string]string{
		"collections": `SELECT COUNT(*) FROM collections WHERE id = ?`,
		"documents":   `SELECT COUNT(*) FROM documents WHERE collection_id = ?`,
		"chunks":      `SELECT COUNT(*) FROM chunks WHERE document_id IN (SELECT id FROM documents WHERE collection_id = ?)`,
		"fts":         `SELECT COUNT(*) FROM documents_fts WHERE rowid = ?`,
		"fast_fields": `SELECT COUNT(*) FROM fast_fields WHERE doc_id = ?`,
	} {
		var n int
		if err := s.db.QueryRow(query, col.ID).Scan(&n); err != nil {
			t.Fatalf("%s count: %v", name, err)
		}
		if n != 0 {
			t.Errorf("%s: expected 0 surviving rows after DeleteCollection, got %d", name, n)
		}
	}
}
