package store

import (
	"context"
	"testing"
)

func TestDeleteOrphansContext_PartialPurge(t *testing.T) {
	s := newTestStore(t)

	col, err := s.CreateCollection("testcol", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	doc1ID, err := s.UpsertDocument(col.ID, "/tmp/doc1.md", "Doc 1", "hash1", 1, 10)
	if err != nil {
		t.Fatalf("UpsertDocument 1: %v", err)
	}
	doc2ID, err := s.UpsertDocument(col.ID, "/tmp/doc2.md", "Doc 2", "hash2", 1, 10)
	if err != nil {
		t.Fatalf("UpsertDocument 2: %v", err)
	}
	doc3ID, err := s.UpsertDocument(col.ID, "/tmp/doc3.md", "Doc 3", "hash3", 1, 10)
	if err != nil {
		t.Fatalf("UpsertDocument 3: %v", err)
	}

	for _, docID := range []int64{doc1ID, doc2ID, doc3ID} {
		if err := s.InsertChunk(docID, 0, "chunk content", nil); err != nil {
			t.Fatalf("InsertChunk: %v", err)
		}
		if err := s.UpsertFTS(docID, "Title", "chunk content"); err != nil {
			t.Fatalf("UpsertFTS: %v", err)
		}
		if err := s.FastFields().Set(docID, "tag", "value"); err != nil {
			t.Fatalf("FastFields.Set: %v", err)
		}
	}

	livePaths := map[string]bool{
		"/tmp/doc1.md": true,
	}

	deleted, err := s.DeleteOrphansContext(context.Background(), col.ID, livePaths)
	if err != nil {
		t.Fatalf("DeleteOrphansContext: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted documents, got %d", deleted)
	}

	// doc1 should survive along with its projections
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE id = ?`, doc1ID).Scan(&count); err != nil || count != 1 {
		t.Errorf("expected doc1 row to survive, count=%d err=%v", count, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chunks WHERE document_id = ?`, doc1ID).Scan(&count); err != nil || count != 1 {
		t.Errorf("expected doc1 chunk to survive, count=%d err=%v", count, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents_fts WHERE rowid = ?`, doc1ID).Scan(&count); err != nil || count != 1 {
		t.Errorf("expected doc1 FTS to survive, count=%d err=%v", count, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM fast_fields WHERE doc_id = ?`, doc1ID).Scan(&count); err != nil || count != 1 {
		t.Errorf("expected doc1 fast field to survive, count=%d err=%v", count, err)
	}

	// doc2 and doc3 should be deleted along with projections
	for _, orphanID := range []int64{doc2ID, doc3ID} {
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE id = ?`, orphanID).Scan(&count); err != nil || count != 0 {
			t.Errorf("expected orphan doc to be deleted, count=%d err=%v", count, err)
		}
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM chunks WHERE document_id = ?`, orphanID).Scan(&count); err != nil || count != 0 {
			t.Errorf("expected orphan chunk to be deleted, count=%d err=%v", count, err)
		}
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents_fts WHERE rowid = ?`, orphanID).Scan(&count); err != nil || count != 0 {
			t.Errorf("expected orphan FTS to be deleted, count=%d err=%v", count, err)
		}
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM fast_fields WHERE doc_id = ?`, orphanID).Scan(&count); err != nil || count != 0 {
			t.Errorf("expected orphan fast field to be deleted, count=%d err=%v", count, err)
		}
	}

	// Repeat purge should be a no-op
	deletedAgain, err := s.DeleteOrphansContext(context.Background(), col.ID, livePaths)
	if err != nil {
		t.Fatalf("DeleteOrphansContext repeat: %v", err)
	}
	if deletedAgain != 0 {
		t.Errorf("expected 0 deleted on repeat purge, got %d", deletedAgain)
	}
}

func TestDeleteOrphansContext_FullPurge(t *testing.T) {
	s := newTestStore(t)

	col, err := s.CreateCollection("testcol2", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	doc1ID, err := s.UpsertDocument(col.ID, "/tmp/doc1.md", "Doc 1", "hash1", 1, 10)
	if err != nil {
		t.Fatalf("UpsertDocument 1: %v", err)
	}
	doc2ID, err := s.UpsertDocument(col.ID, "/tmp/doc2.md", "Doc 2", "hash2", 1, 10)
	if err != nil {
		t.Fatalf("UpsertDocument 2: %v", err)
	}

	for _, docID := range []int64{doc1ID, doc2ID} {
		if err := s.InsertChunk(docID, 0, "chunk content", nil); err != nil {
			t.Fatalf("InsertChunk: %v", err)
		}
		if err := s.UpsertFTS(docID, "Title", "chunk content"); err != nil {
			t.Fatalf("UpsertFTS: %v", err)
		}
		if err := s.FastFields().Set(docID, "tag", "value"); err != nil {
			t.Fatalf("FastFields.Set: %v", err)
		}
	}

	// Full purge with livePaths == nil
	deleted, err := s.DeleteOrphansContext(context.Background(), col.ID, nil)
	if err != nil {
		t.Fatalf("DeleteOrphansContext full purge: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted documents on full purge, got %d", deleted)
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE collection_id = ?`, col.ID).Scan(&count); err != nil || count != 0 {
		t.Errorf("expected 0 documents after full purge, count=%d err=%v", count, err)
	}
}
