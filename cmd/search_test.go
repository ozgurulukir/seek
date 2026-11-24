package cmd_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ozgurulukir/seek/cmd"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
	"github.com/ozgurulukir/seek/internal/store/storetest"
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

func TestSearchCmd_AnalyzeLangFromConfig(t *testing.T) {
	// With no --analyze-lang flag, search.analyze_lang from config must drive
	// analysis (Jerry's rec: the language should be configurable without a CLI flag).
	cfg := &config.AppConfig{}
	cfg.Config.Search.AnalyzeLang = "tr"

	searchCmd := &cmd.SearchCmd{
		Query:   "kitaplar",
		Analyze: true, // AnalyzeLang left empty -> resolved from config
	}

	out := captureStdout(t, func() {
		if err := searchCmd.Run(cfg); err != nil {
			t.Errorf("SearchCmd.Run analyze mode failed: %v", err)
		}
	})
	if !strings.Contains(out, "Analyzed (tr)") {
		t.Errorf("expected analyzer language resolved from config to be tr, got: %s", out)
	}
	if !strings.Contains(out, "kitap") {
		t.Errorf("expected Turkish stemming of kitaplar -> kitap, got: %s", out)
	}
}

func TestSearchCmd_AnalyzeLangFlagOverridesConfig(t *testing.T) {
	// An explicit --analyze-lang must win over config.
	cfg := &config.AppConfig{}
	cfg.Config.Search.AnalyzeLang = "tr"

	searchCmd := &cmd.SearchCmd{
		Query:       "running searches",
		Analyze:     true,
		AnalyzeLang: "en",
	}

	out := captureStdout(t, func() {
		if err := searchCmd.Run(cfg); err != nil {
			t.Errorf("SearchCmd.Run analyze mode failed: %v", err)
		}
	})
	if !strings.Contains(out, "Analyzed (en)") {
		t.Errorf("expected explicit --analyze-lang en to override config tr, got: %s", out)
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
		storetest.SkipIfFTS5Disabled(t, err)
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
	if err := db.UpsertFTSContext(context.Background(), docID, "Test Doc", content); err != nil {
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

	// 3. Multi-word autocomplete test
	multiAutoCmd := &cmd.SearchCmd{
		Query:        "find sea",
		Autocomplete: true,
	}
	out = captureStdout(t, func() {
		if err := multiAutoCmd.Run(cfg); err != nil {
			t.Errorf("SearchCmd.Run multi-word autocomplete failed: %v", err)
		}
	})
	if !strings.Contains(out, "find search") {
		t.Errorf("autocomplete output missing 'find search' suggestion, got: %q", out)
	}
}

// TestSearchCmd_AutocompleteClampsMax proves the CLI path applies the same
// 100-result ceiling as MCP, so a huge --autocomplete-max cannot materialize
// the entire vocabulary.
func TestSearchCmd_AutocompleteClampsMax(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		storetest.SkipIfFTS5Disabled(t, err)
		t.Fatal(err)
	}
	defer db.Close()
	col, err := db.CreateCollection("notes", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := db.UpsertDocument(col.ID, "/tmp/vocab.md", "Vocab", "h1", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < 150; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "term%03d", i)
	}
	if err := db.UpsertFTSContext(context.Background(), docID, "Vocab", b.String()); err != nil {
		t.Fatal(err)
	}

	cfg := &config.AppConfig{DBPath: dbPath}
	autoCmd := &cmd.SearchCmd{
		Query:           "term",
		Autocomplete:    true,
		AutocompleteMax: 10000,
	}
	out := captureStdout(t, func() {
		if err := autoCmd.Run(cfg); err != nil {
			t.Errorf("SearchCmd.Run autocomplete failed: %v", err)
		}
	})
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "term") {
			count++
		}
	}
	if count != 100 {
		t.Fatalf("autocomplete suggestions = %d, want the 100-result ceiling; output: %q", count, out)
	}
}
