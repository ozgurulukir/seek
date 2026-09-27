package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkDeleteOrphans(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	col, err := s.CreateCollection("bench-col", "markdown", "/tmp", "*.md")
	if err != nil {
		b.Fatalf("CreateCollection: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		const numDocs = 500
		for d := 0; d < numDocs; d++ {
			docPath := fmt.Sprintf("/tmp/doc_%d_%d.md", i, d)
			docID, err := s.UpsertDocument(col.ID, docPath, "Title", "hash", 1.0, 10)
			if err != nil {
				b.Fatalf("UpsertDocument: %v", err)
			}
			if err := s.InsertChunk(docID, 0, "Chunk content for document", nil); err != nil {
				b.Fatalf("InsertChunk: %v", err)
			}
			if err := s.UpsertFTS(docID, "Title", "Chunk content for document"); err != nil {
				b.Fatalf("UpsertFTS: %v", err)
			}
			if err := s.FastFields().Set(docID, "topic", "testing"); err != nil {
				b.Fatalf("FastFields.Set: %v", err)
			}
		}
		b.StartTimer()

		removed, err := s.DeleteOrphansContext(context.Background(), col.ID, map[string]bool{})
		if err != nil {
			b.Fatalf("DeleteOrphansContext: %v", err)
		}
		if removed != numDocs {
			b.Fatalf("removed = %d, want %d", removed, numDocs)
		}
	}
}
