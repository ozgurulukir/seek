package cmd_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ozgurulukir/seek/cmd"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

// captureStdout runs fn while capturing everything it writes to stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

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

	const content = "This is a search result content test"

	err = db.InsertChunkWithLines(docID, 0, content, 1, 5, []float32{0.1, 0.2, 0.3, 0.4})
	if err != nil {
		t.Fatal(err)
	}

	// Populate the FTS index; without this BM25 finds nothing.
	if err := db.UpsertFTS(docID, "Test Doc", content); err != nil {
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

	out := captureStdout(t, func() {
		if err := searchCmd.Run(cfg); err != nil {
			t.Errorf("SearchCmd.Run failed: %v", err)
		}
	})
	if !strings.Contains(out, "test.md") {
		t.Errorf("BM25 search output missing seeded document path, got: %q", out)
	}
	if strings.Contains(out, "No results found.") {
		t.Errorf("BM25 search returned no results despite matching seed data")
	}

	// 2. Autocomplete mode (requires chunks with non-nil embeddings)
	autoCmd := &cmd.SearchCmd{
		Query:        "sear",
		Autocomplete: true,
	}
	out = captureStdout(t, func() {
		if err := autoCmd.Run(cfg); err != nil {
			t.Errorf("SearchCmd.Run autocomplete mode failed: %v", err)
		}
	})
	if !strings.Contains(out, "search") {
		t.Errorf("autocomplete output missing 'search' suggestion, got: %q", out)
	}
}
