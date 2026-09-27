package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkDeleteOrphansContext(b *testing.B) {
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dbPath := filepath.Join(b.TempDir(), "bench_orphans.db")
		s, err := Open(dbPath)
		if err != nil {
			b.Fatalf("Open: %v", err)
		}

		col, err := s.CreateCollection("bench_col", CollectionTypeCode, "/bench", "*.go")
		if err != nil {
			s.Close()
			b.Fatalf("CreateCollection: %v", err)
		}

		// Insert 500 documents with chunks, fts, fast_fields
		for j := 0; j < 500; j++ {
			docPath := fmt.Sprintf("/bench/doc_%d.go", j)
			docID, err := s.UpsertDocument(col.ID, docPath, fmt.Sprintf("Doc %d", j), "hash", 1000, 10)
			if err != nil {
				s.Close()
				b.Fatalf("UpsertDocument: %v", err)
			}
			if err := s.FastFields().Set(docID, "lang", "go"); err != nil {
				s.Close()
				b.Fatalf("FastFields Set: %v", err)
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO documents_fts(rowid, title, content) VALUES(?, ?, ?)`, docID, fmt.Sprintf("Doc %d", j), "package main"); err != nil {
				s.Close()
				b.Fatalf("documents_fts insert: %v", err)
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO chunks(document_id, seq, content) VALUES(?, 0, 'package main')`, docID); err != nil {
				s.Close()
				b.Fatalf("chunks insert: %v", err)
			}
		}

		b.StartTimer()
		// Clean up all 500 documents as orphans (nil livePaths means all are orphans)
		n, err := s.DeleteOrphansContext(ctx, col.ID, nil)
		b.StopTimer()

		if err != nil {
			s.Close()
			b.Fatalf("DeleteOrphansContext: %v", err)
		}
		if n != 500 {
			s.Close()
			b.Fatalf("expected 500 deleted, got %d", n)
		}

		s.Close()
	}
}
