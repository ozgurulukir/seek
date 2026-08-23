package cmd_test

import (
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ozgurulukir/seek/cmd"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

func TestSearchCmd_Analyze(t *testing.T) {
	searchCmd := &cmd.SearchCmd{
		Query:       "running searches",
		Analyze:     true,
		AnalyzeLang: "en",
	}

	cfg := &config.AppConfig{}
	if err := searchCmd.Run(cfg); err != nil {
		t.Fatalf("SearchCmd.Run analyze mode failed: %v", err)
	}
}

func TestSearchCmd_Execution(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.AppConfig{
		DBPath: dbPath,
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	col, err := db.CreateCollection("test-col", store.CollectionTypeDocuments, "/path/to/doc", "**/*")
	if err != nil {
		t.Fatal(err)
	}

	docID, err := db.UpsertDocument(col.ID, "/path/to/doc/test.md", "Test Doc", "hash1", 1000, 100)
	if err != nil {
		t.Fatal(err)
	}

	err = db.InsertChunkWithLines(docID, 0, "This is a search result content test", 1, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// 1. BM25 search with filters and context expansion
	searchCmd := &cmd.SearchCmd{
		Query:      "search content",
		Lex:        true,
		Limit:      5,
		Collection: "test-col",
		DocType:    "documents",
		Context:    1,
	}

	if err := searchCmd.Run(cfg); err != nil {
		t.Fatalf("SearchCmd.Run failed: %v", err)
	}

	// 2. Autocomplete mode
	autoCmd := &cmd.SearchCmd{
		Query:        "sear",
		Autocomplete: true,
	}
	if err := autoCmd.Run(cfg); err != nil {
		t.Fatalf("SearchCmd.Run autocomplete mode failed: %v", err)
	}
}
