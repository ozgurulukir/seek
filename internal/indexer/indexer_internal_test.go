package indexer

import (
	"context"
	"os"
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

	idx.writeFastFields(docID, "n.md", map[string]string{
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

// cleanupTestIndexer wires a real SQLite store and a fresh collection so the
// guarded orphan cleanup can be exercised end to end.
func cleanupTestIndexer(t *testing.T) (*Indexer, *store.Store, *store.Collection) {
	t.Helper()
	tmp := t.TempDir()
	db, err := store.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags \"fts5 sqlite_fts5\"")
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	root := filepath.Join(tmp, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	col, err := db.CreateCollection("test", store.CollectionTypeDocuments, root, "")
	if err != nil {
		t.Fatal(err)
	}
	idx := New(cfgFromTest(t, tmp, filepath.Join(tmp, "test.db")), db)
	idx.WithLogger(nopLogger{})
	return idx, db, col
}

func seedDoc(t *testing.T, db *store.Store, col *store.Collection, path string) {
	t.Helper()
	if _, err := db.UpsertAndReplaceIndex(context.Background(), store.DocumentIndex{
		CollectionID: col.ID,
		Path:         path,
		Title:        filepath.Base(path),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSafeCleanupOrphansNilPurges(t *testing.T) {
	idx, db, col := cleanupTestIndexer(t)
	seedDoc(t, db, col, filepath.Join(col.Path, "stale.md"))

	removed, err := idx.safeCleanupOrphans(col, nil, "documents")
	if err != nil {
		t.Fatalf("nil livePaths: %v", err)
	}
	if removed != 1 {
		t.Fatalf("nil livePaths removed = %d, want 1", removed)
	}
	docs, _ := db.ListDocumentPaths(col.ID)
	if len(docs) != 0 {
		t.Fatalf("nil livePaths must purge collection, %d documents remain", len(docs))
	}
}

func TestSafeCleanupOrphansMissingRootPreserves(t *testing.T) {
	idx, db, col := cleanupTestIndexer(t)
	seedDoc(t, db, col, filepath.Join(col.Path, "stale.md"))

	var logBuf string
	idx.WithLogger(captureLogger{&logBuf})

	// Simulate an unmounted/renamed scan root: path no longer exists.
	col.Path = filepath.Join(col.Path, "gone")

	removed, err := idx.safeCleanupOrphans(col, map[string]bool{}, "documents")
	if err != nil {
		t.Fatalf("missing root: %v", err)
	}
	if removed != 0 {
		t.Fatalf("missing root removed = %d, want 0", removed)
	}
	docs, _ := db.ListDocumentPaths(col.ID)
	if len(docs) != 1 {
		t.Fatalf("missing root wiped documents: %d remain, want 1", len(docs))
	}
	if !strings.Contains(logBuf, "scan root unavailable") {
		t.Fatalf("missing root did not warn: %q", logBuf)
	}
}

func TestSafeCleanupOrphansEmptyScanPurges(t *testing.T) {
	idx, db, col := cleanupTestIndexer(t)
	seedDoc(t, db, col, filepath.Join(col.Path, "stale.md"))

	// Source files are the source of truth: a legitimate empty scan of an
	// existing directory (e.g. the user deleted every file) must purge so the
	// index reflects the deletion.
	removed, err := idx.safeCleanupOrphans(col, map[string]bool{}, "documents")
	if err != nil {
		t.Fatalf("empty scan: %v", err)
	}
	if removed != 1 {
		t.Fatalf("empty scan removed = %d, want 1", removed)
	}
	docs, _ := db.ListDocumentPaths(col.ID)
	if len(docs) != 0 {
		t.Fatalf("empty scan must purge, %d documents remain", len(docs))
	}
}

func TestSafeCleanupOrphansRemovesOrphans(t *testing.T) {
	idx, db, col := cleanupTestIndexer(t)
	live := filepath.Join(col.Path, "live.md")
	stale := filepath.Join(col.Path, "stale.md")
	seedDoc(t, db, col, live)
	seedDoc(t, db, col, stale)

	removed, err := idx.safeCleanupOrphans(col, map[string]bool{live: true}, "documents")
	if err != nil {
		t.Fatalf("normal cleanup: %v", err)
	}
	if removed != 1 {
		t.Fatalf("normal cleanup removed = %d, want 1", removed)
	}
	docs, _ := db.ListDocumentPaths(col.ID)
	if _, ok := docs[live]; !ok {
		t.Fatal("live document was removed")
	}
	if _, ok := docs[stale]; ok {
		t.Fatal("stale document was not removed")
	}
}

func TestSafeCleanupOrphansFileRootPreserves(t *testing.T) {
	idx, db, col := cleanupTestIndexer(t)
	seedDoc(t, db, col, filepath.Join(col.Path, "stale.md"))

	// A file (not a directory) is an incomplete scan root: never purge.
	filePath := filepath.Join(col.Path, "notadir.md")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	col.Path = filePath

	var logBuf string
	idx.WithLogger(captureLogger{&logBuf})

	removed, err := idx.safeCleanupOrphans(col, map[string]bool{}, "documents")
	if err != nil {
		t.Fatalf("file root: %v", err)
	}
	if removed != 0 {
		t.Fatalf("file root removed = %d, want 0", removed)
	}
	docs, _ := db.ListDocumentPaths(col.ID)
	if len(docs) != 1 {
		t.Fatalf("file root wiped documents: %d remain, want 1", len(docs))
	}
	if !strings.Contains(logBuf, "not a directory") {
		t.Fatalf("file root did not warn: %q", logBuf)
	}
}
