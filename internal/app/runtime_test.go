package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

func TestOpenRuntimeSharesStoreWithSearch(t *testing.T) {
	cfg := &config.AppConfig{
		Config: config.Config{VectorIndex: config.VectorIndexConfig{Backend: "linear"}},
		DBPath: filepath.Join(t.TempDir(), "seek.db"),
	}
	runtime, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	collection, err := runtime.Store.CreateCollection("notes", store.CollectionTypeMarkdown, t.TempDir(), "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	documentID, err := runtime.Store.UpsertDocument(collection.ID, "note.md", "Architecture", "hash", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if err := runtime.Store.UpsertFTS(documentID, "Architecture", "runtime composition"); err != nil {
		t.Fatalf("UpsertFTS: %v", err)
	}

	results, err := runtime.Search.SearchBM25(context.Background(), "composition", 10, search.Options{})
	if err != nil {
		t.Fatalf("SearchBM25: %v", err)
	}
	if len(results) != 1 || results[0].DocumentID != documentID {
		t.Fatalf("SearchBM25 results = %+v, want document %d", results, documentID)
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
