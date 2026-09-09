package indexer

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/extractor"
	"github.com/ozgurulukir/seek/internal/store"
)

type nopLogger struct{}

func (nopLogger) Printf(format string, v ...interface{}) {}

type mockExtractor struct{}

func (mockExtractor) Extract(ctx context.Context, path string) (extractor.Result, error) {
	return extractor.Result{}, nil
}

func (mockExtractor) Supports(path string) bool { return true }
func (mockExtractor) Name() string              { return "mock" }

func TestIndexer_WithExtractor(t *testing.T) {
	cfg := &config.AppConfig{}
	idx := New(cfg, nil)

	mockExt := &mockExtractor{}

	// Check fluent return
	returnedIdx := idx.WithExtractor(mockExt)
	if returnedIdx != idx {
		t.Errorf("WithExtractor did not return the indexer instance")
	}

	// Check internal state
	if idx.ext != mockExt {
		t.Errorf("WithExtractor did not set the extractor correctly")
	}

	// Check extractorFor resolution priority
	col := &store.Collection{Backend: "some-backend"}
	resolvedExt, err := idx.extractorFor(col)
	if err != nil {
		t.Fatalf("extractorFor returned unexpected error: %v", err)
	}

	if resolvedExt != mockExt {
		t.Errorf("extractorFor did not prioritize the overridden extractor, got: %v", resolvedExt)
	}

	// Revert the override
	idx.WithExtractor(nil)
	if idx.ext != nil {
		t.Errorf("WithExtractor did not revert the extractor to nil")
	}
}

func TestWriteFastFields(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled")
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmp, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := db.UpsertDocument(col.ID, "n.md", "Note", "h", 1, 2)
	if err != nil {
		t.Fatal(err)
	}

	idx := New(cfgFromTest(t, tmp, filepath.Join(tmp, "test.db")), db)
	idx.WithLogger(nopLogger{}) // ignore the small diagnostic output

	idx.writeFastFields(docID, map[string]string{
		"tag":  "go,rust",
		"date": "2026-01-15",
		"":     "skipped-empty-key", // empty key must be ignored
	})

	// Verify written values survive.
	for key, want := range map[string]string{"tag": "go,rust", "date": "2026-01-15"} {
		gotVal, err := db.FastFields().Get(docID, key)
		if err != nil {
			t.Fatalf("Get(%q): %v", key, err)
		}
		got, _ := gotVal.(string)
		if got != want {
			t.Errorf("fast field %q = %q, want %q", key, got, want)
		}
	}
}

// cfgFromTest builds a minimal AppConfig for New().
func cfgFromTest(t *testing.T, cacheDir, dbPath string) *config.AppConfig {
	t.Helper()
	return &config.AppConfig{
		Config:   config.Config{},
		CacheDir: cacheDir,
		DBPath:   dbPath,
	}
}

func TestSyncHandlersRegistry(t *testing.T) {
	// Every registered type must have a handler; unknown types must error.
	if got := len(syncHandlers); got != 8 {
		t.Errorf("expected 8 registered sync handlers, got %d", got)
	}

	types := []store.CollectionType{
		store.CollectionTypeMarkdown, store.CollectionTypeClaude,
		store.CollectionTypeCodex, store.CollectionTypeImages,
		store.CollectionTypePDF, store.CollectionTypeDocuments,
		store.CollectionTypeCode, store.CollectionTypeParser,
	}
	for _, typ := range types {
		if _, ok := syncHandlers[typ]; !ok {
			t.Errorf("missing handler for %q", typ)
		}
	}

	// Unknown type flows through SyncCollection and errors with the same
	// message the previous switch produced.
	tmp := t.TempDir()
	db, err := store.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled")
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	idx := New(cfgFromTest(t, tmp, filepath.Join(tmp, "test.db")), db)
	col := &store.Collection{Type: store.CollectionType("nosuchtype")}
	if err := idx.SyncCollection(col); err == nil {
		t.Error("unknown collection type must return an error")
	} else if !strings.Contains(err.Error(), "unknown collection type") {
		t.Errorf("unexpected error: %v", err)
	}
}
