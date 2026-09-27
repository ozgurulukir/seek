package store

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkDeleteOrphansContext(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tmp := b.TempDir()
		s, err := Open(tmp + "/test.db")
		if err != nil {
			b.Fatalf("Open: %v", err)
		}

		ctx := context.Background()
		col, err := s.CreateCollection("test", CollectionTypeDocuments, "/tmp/test", "")
		if err != nil {
			s.Close()
			b.Fatalf("CreateCollection: %v", err)
		}

		numDocs := 500
		for d := 0; d < numDocs; d++ {
			docPath := fmt.Sprintf("/tmp/test/doc_%d.txt", d)
			_, err := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
				CollectionID: col.ID,
				Path:         docPath,
				Title:        fmt.Sprintf("Doc %d", d),
				FTSContent:   "This is sample document text for full text search indexing.",
				Chunks: []IndexChunk{
					{Seq: 0, Content: "chunk 0 content"},
					{Seq: 1, Content: "chunk 1 content"},
				},
				FastFields: map[string]string{
					"lang": "go",
					"tag":  "test",
				},
			})
			if err != nil {
				s.Close()
				b.Fatalf("UpsertAndReplaceIndex: %v", err)
			}
		}

		livePaths := make(map[string]bool)

		b.StartTimer()
		removed, err := s.DeleteOrphansContext(ctx, col.ID, livePaths)
		b.StopTimer()

		if err != nil {
			s.Close()
			b.Fatalf("DeleteOrphansContext: %v", err)
		}
		if removed != numDocs {
			s.Close()
			b.Fatalf("expected %d removed, got %d", numDocs, removed)
		}

		s.Close()
	}
}
