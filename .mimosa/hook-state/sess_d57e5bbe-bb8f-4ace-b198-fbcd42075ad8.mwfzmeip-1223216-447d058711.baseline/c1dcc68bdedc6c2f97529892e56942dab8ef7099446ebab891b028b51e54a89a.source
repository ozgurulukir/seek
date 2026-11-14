package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkDeleteOrphansFullReindex(b *testing.B) {
	for _, docCount := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("docs_%d", docCount), func(b *testing.B) {
			b.StopTimer()
			s, err := Open(filepath.Join(b.TempDir(), "bench.db"))
			if err != nil {
				b.Fatalf("Open: %v", err)
			}
			defer s.Close()

			col, err := s.CreateCollection("bench-col", "markdown", "/tmp", "**/*.md")
			if err != nil {
				b.Fatalf("CreateCollection: %v", err)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				// Populate database before each measured iteration
				for j := 0; j < docCount; j++ {
					path := fmt.Sprintf("/tmp/doc_%d.md", j)
					docID, err := s.UpsertDocument(col.ID, path, fmt.Sprintf("Doc %d", j), "hash", 1, 10)
					if err != nil {
						b.Fatalf("UpsertDocument: %v", err)
					}
					_ = s.InsertChunk(docID, 0, "some text content", nil)
					_ = s.UpsertFTS(docID, "title", "some text content")
					_ = s.FastFields().Set(docID, "language", "en")
				}
				b.StartTimer()

				n, err := s.DeleteOrphansContext(context.Background(), col.ID, nil)
				if err != nil {
					b.Fatalf("DeleteOrphansContext: %v", err)
				}
				if n != docCount {
					b.Fatalf("deleted = %d, want %d", n, docCount)
				}
			}
		})
	}
}

func BenchmarkDeleteOrphansPartial(b *testing.B) {
	for _, docCount := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("docs_%d", docCount), func(b *testing.B) {
			b.StopTimer()
			s, err := Open(filepath.Join(b.TempDir(), "bench.db"))
			if err != nil {
				b.Fatalf("Open: %v", err)
			}
			defer s.Close()

			col, err := s.CreateCollection("bench-col", "markdown", "/tmp", "**/*.md")
			if err != nil {
				b.Fatalf("CreateCollection: %v", err)
			}

			// livePaths maps only 10% of documents as live, making 90% orphans
			liveCount := docCount / 10

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				livePaths := make(map[string]bool, liveCount)
				for j := 0; j < docCount; j++ {
					path := fmt.Sprintf("/tmp/doc_%d.md", j)
					docID, err := s.UpsertDocument(col.ID, path, fmt.Sprintf("Doc %d", j), "hash", 1, 10)
					if err != nil {
						b.Fatalf("UpsertDocument: %v", err)
					}
					_ = s.InsertChunk(docID, 0, "some text content", nil)
					_ = s.UpsertFTS(docID, "title", "some text content")
					_ = s.FastFields().Set(docID, "language", "en")
					if j < liveCount {
						livePaths[path] = true
					}
				}
				b.StartTimer()

				n, err := s.DeleteOrphansContext(context.Background(), col.ID, livePaths)
				if err != nil {
					b.Fatalf("DeleteOrphansContext: %v", err)
				}
				wantOrphans := docCount - liveCount
				if n != wantOrphans {
					b.Fatalf("deleted = %d, want %d", n, wantOrphans)
				}
			}
		})
	}
}
