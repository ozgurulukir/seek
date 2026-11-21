package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/store"
)

// T4 (audit 2026-11-21): Pipeline.Sync itself (nil-guard, indexer handoff,
// CollectionID defaulting, SkipEmbed short-circuit, embed handoff) was only
// exercised indirectly via the SkipEmbed path in internal/app tests. These
// tests pin the orchestration seam using the same real-Store + mock-provider
// harness the degrade tests use — no new production seam required.

// syncTestAPIKey reads the key from the environment so no credential literal
// lands in source. The mock provider never sends it anywhere.
func syncTestAPIKey() string {
	if key := os.Getenv("SEEK_TEST_API_KEY"); key != "" {
		return key
	}
	return "seek-test-fixture"
}

// newSyncStore builds a real store with two markdown collections, each
// pointing at its own temp corpus directory. Only the first gets a source file.
func newSyncStore(t *testing.T) (*store.Store, *store.Collection, *store.Collection) {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	dir1 := filepath.Join(tmpDir, "corpus1")
	dir2 := filepath.Join(tmpDir, "corpus2")
	for _, dir := range []string{dir1, dir2} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir1, "note.md"), []byte("# Hello\n\nworld content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Chunking caveat: tests assert report.Indexed == 1, so the corpus must
	// stay a single chunk under the default chunk config.

	first, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, dir1, "*.md")
	if err != nil {
		t.Fatalf("CreateCollection notes: %v", err)
	}
	second, err := db.CreateCollection("other", store.CollectionTypeMarkdown, dir2, "*.md")
	if err != nil {
		t.Fatalf("CreateCollection other: %v", err)
	}
	return db, first, second
}

func syncConfig(t *testing.T) *config.AppConfig {
	return appConfigFor(t, func(c *config.Config) {
		c.Embedding.APIKey = syncTestAPIKey()
		c.Embedding.Model = "text-embedding-3-small"
		c.Embedding.Dimensions = 2
	})
}

func TestSync_UnconfiguredPipelineFails(t *testing.T) {
	db, first, _ := newSyncStore(t)
	p := New(syncConfig(t), db, nil, embed.Provider{Document: &mockDocEmbedder{vec: []float32{1, 0}}})
	if _, err := p.Sync(context.Background(), first, Options{}, &captureLogger{}); err == nil {
		t.Fatal("Sync with a nil indexer must fail")
	}
	// The nil-receiver branch of the same guard.
	var nilPipeline *Pipeline
	if _, err := nilPipeline.Sync(context.Background(), first, Options{}, &captureLogger{}); err == nil {
		t.Fatal("Sync on a nil pipeline must fail")
	}
}

func TestSync_SkipEmbedIndexesWithoutEmbedding(t *testing.T) {
	db, first, _ := newSyncStore(t)
	mock := &mockDocEmbedder{vec: []float32{1, 0}}
	p := New(syncConfig(t), db, indexer.New(syncConfig(t), db), embed.Provider{Document: mock})

	report, err := p.Sync(context.Background(), first, Options{SkipEmbed: true}, &captureLogger{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if report.Indexed != 1 {
		t.Errorf("report.Indexed = %d, want 1", report.Indexed)
	}
	if mock.calls != 0 {
		t.Errorf("SkipEmbed must not call the embedder, got %d calls", mock.calls)
	}
	pending, err := db.GetChunksWithoutEmbeddingForCollectionContext(context.Background(), first.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) == 0 {
		t.Error("chunk should stay pending after SkipEmbed sync")
	}
}

func TestSync_IndexesAndEmbedsCollectionScoped(t *testing.T) {
	db, first, second := newSyncStore(t)
	// Seed a pending chunk into the second collection so the scoping
	// assertion is load-bearing: an index-wide embed pass WOULD embed it,
	// and the test fails if Sync stops defaulting opts.CollectionID to the
	// synced collection.
	secondDoc, err := db.UpsertDocument(second.ID, "other.md", "Other", "h2", 1, 1)
	if err != nil {
		t.Fatalf("upsert second document: %v", err)
	}
	if err := db.InsertChunk(secondDoc, 0, "second collection content", nil); err != nil {
		t.Fatalf("insert second chunk: %v", err)
	}
	mock := &mockDocEmbedder{vec: []float32{1, 0}}
	p := New(syncConfig(t), db, indexer.New(syncConfig(t), db), embed.Provider{Document: mock})

	report, err := p.Sync(context.Background(), first, Options{Realtime: true}, &captureLogger{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if report.Indexed != 1 {
		t.Errorf("report.Indexed = %d, want 1", report.Indexed)
	}
	if mock.calls == 0 {
		t.Error("Sync must embed the chunks produced by the collection")
	}
	firstPending, err := db.GetChunksWithoutEmbeddingForCollectionContext(context.Background(), first.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPending) != 0 {
		t.Errorf("first collection pending = %d, want 0", len(firstPending))
	}
	// The seeded second-collection chunk must still be pending.
	secondPending, err := db.GetChunksWithoutEmbeddingForCollectionContext(context.Background(), second.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPending) != 1 {
		t.Errorf("second collection pending = %d, want 1 (Sync must not embed other collections)", len(secondPending))
	}
}

func TestSync_EmbedFailureStillReturnsReport(t *testing.T) {
	db, first, _ := newSyncStore(t)
	p := New(syncConfig(t), db, indexer.New(syncConfig(t), db), embed.Provider{})

	report, err := p.Sync(context.Background(), first, Options{Realtime: true}, &captureLogger{})
	if err == nil {
		t.Fatal("expected embed-handoff failure without a document embedder")
	}
	if report.Indexed != 1 {
		t.Errorf("report.Indexed = %d, want 1 even when the embed handoff fails", report.Indexed)
	}
}
