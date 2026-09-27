package store

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkDeleteOrphansContext(b *testing.B) {
	b.ReportAllocs()
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s := newTestStore(&testing.T{})
		col, err := s.CreateCollection("bench", CollectionTypeMarkdown, "/bench", "*.md")
		if err != nil {
			b.Fatal(err)
		}

		numDocs := 500
		for d := 0; d < numDocs; d++ {
			docPath := fmt.Sprintf("file_%d.md", d)
			_, err := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
				CollectionID: col.ID,
				Path:         docPath,
				Title:        fmt.Sprintf("Title %d", d),
				ContentHash:  "hash",
				Mtime:        1.0,
				LineCount:    10,
				FTSContent:   fmt.Sprintf("Searchable content for file %d", d),
				Chunks: []IndexChunk{
					{Seq: 0, Content: fmt.Sprintf("Searchable content for file %d", d), StartLine: 1, EndLine: 10},
				},
				FastFields: map[string]string{"lang": "go"},
			})
			if err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()

		// Delete all 500 documents as orphans (livePaths = nil)
		removed, err := s.DeleteOrphansContext(ctx, col.ID, nil)
		if err != nil {
			b.Fatal(err)
		}
		if removed != numDocs {
			b.Fatalf("removed = %d, want %d", removed, numDocs)
		}
		s.Close()
	}
}
