package search

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
)

func TestEngine_SearchWithOptions(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags fts5 ./... or make test")
		}
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	engine := NewEngine(s, nil)

	col, err := s.CreateCollection("test-col", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}
	docID1, err := s.UpsertDocument(col.ID, "/tmp/doc1.md", "doc1", "hash1", 1, 1)
	if err != nil {
		t.Fatalf("failed to upsert document: %v", err)
	}
	err = s.InsertChunk(docID1, 0, "hello world search text", nil)
	if err != nil {
		t.Fatalf("failed to insert chunk: %v", err)
	}
	err = s.UpsertFTS(docID1, "doc1", "hello world search text")
	if err != nil {
		t.Fatalf("failed to update FTS: %v", err)
	}

	docID2, err := s.UpsertDocument(col.ID, "/tmp/doc2.md", "doc2", "hash2", 1, 1)
	if err != nil {
		t.Fatalf("failed to upsert document: %v", err)
	}
	err = s.InsertChunk(docID2, 0, "hello again testing", nil)
	if err != nil {
		t.Fatalf("failed to insert chunk: %v", err)
	}
	err = s.UpsertFTS(docID2, "doc2", "hello again testing")
	if err != nil {
		t.Fatalf("failed to update FTS: %v", err)
	}

	tests := []struct {
		name       string
		query      string
		opts       Options
		wantResult int
	}{
		{
			name:  "default limit when 0",
			query: "hello",
			opts: Options{
				Limit: 0,
			},
			wantResult: 2, // both docs have "hello"
		},
		{
			name:  "custom limit",
			query: "hello",
			opts: Options{
				Limit: 1,
			},
			wantResult: 1, // limit to 1
		},
		{
			name:  "negative limit",
			query: "hello",
			opts: Options{
				Limit: -5,
			},
			wantResult: 2, // should use default limit, which is 20, returning both docs
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := engine.SearchWithOptions(context.Background(), tt.query, tt.opts)
			if err != nil {
				t.Fatalf("SearchWithOptions() error = %v", err)
			}

			if len(res) != tt.wantResult {
				t.Errorf("SearchWithOptions() returned %d results, want %d", len(res), tt.wantResult)
			}
		})
	}
}
