package store_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
)

func TestGetSurroundingContext(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags fts5 ./... or make test")
		}
		t.Fatal(err)
	}
	defer db.Close()

	col, err := db.CreateCollection("test-col", store.CollectionTypeCode, tmpDir, "**/*")
	if err != nil {
		t.Fatal(err)
	}

	docID, err := db.UpsertDocument(col.ID, "/path/to/main.go", "main.go", "hash1", 1000, 100)
	if err != nil {
		t.Fatal(err)
	}

	// Insert 3 sequential chunks with line numbers
	if err := db.InsertChunkWithLines(docID, 0, "chunk 0 content", 1, 30, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertChunkWithLines(docID, 1, "chunk 1 content", 31, 60, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertChunkWithLines(docID, 2, "chunk 2 content", 61, 90, nil); err != nil {
		t.Fatal(err)
	}

	// Radius 0: only chunk 1
	ctx0, sLine, eLine, err := db.GetSurroundingContext(docID, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ctx0 != "chunk 1 content" || sLine != 31 || eLine != 60 {
		t.Errorf("radius 0: got %q (%d-%d)", ctx0, sLine, eLine)
	}

	// Radius 1: chunks 0, 1, 2
	ctx1, sLine1, eLine1, err := db.GetSurroundingContext(docID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	expected := "chunk 0 content\n\nchunk 1 content\n\nchunk 2 content"
	if ctx1 != expected || sLine1 != 1 || eLine1 != 90 {
		t.Errorf("radius 1: got %q (%d-%d), want %q (1-90)", ctx1, sLine1, eLine1, expected)
	}
}
