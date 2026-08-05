package indexer_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anthropics/seek/internal/config"
	"github.com/anthropics/seek/internal/indexer"
	"github.com/anthropics/seek/internal/store"
)

type nopLogger struct{}

func (nopLogger) Printf(format string, v ...interface{}) {}

func TestIndexer_IncrementalSync(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &config.AppConfig{
		DBPath: dbPath,
	}

	idx := indexer.New(cfg, db).WithLogger(nopLogger{})

	col, err := db.CreateCollection("test-notes", "markdown", tmpDir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Initial sync (empty)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	// 2. Add a file
	doc1Path := filepath.Join(tmpDir, "doc1.md")
	os.WriteFile(doc1Path, []byte("# Title\n\nSome content here."), 0644)

	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	docs, _ := db.ListDocumentPaths(col.ID)
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}

	_ = docs[doc1Path]
	chunks, _ := db.GetChunksWithoutEmbedding(true)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	// 3. Unchanged sync (should skip)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	chunks2, _ := db.GetChunksWithoutEmbedding(true)
	if len(chunks2) != 1 {
		t.Fatalf("expected chunk count to remain 1, got %d", len(chunks2))
	}

	// 4. Update file
	time.Sleep(10 * time.Millisecond) // ensure mtime changes
	os.WriteFile(doc1Path, []byte("# Title\n\nUpdated content."), 0644)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	docsAfter, _ := db.ListDocumentPaths(col.ID)
	if len(docsAfter) != 1 {
		t.Fatalf("expected 1 doc after update, got %d", len(docsAfter))
	}
	chunks3, _ := db.GetChunksWithoutEmbedding(true)
	if len(chunks3) != 1 {
		t.Fatalf("expected 1 chunk after update, got %d", len(chunks3))
	}
	if chunks3[0].Content != "# Title\n\nUpdated content." {
		t.Fatalf("expected updated chunk content, got %q", chunks3[0].Content)
	}

	// 5. Remove file
	os.Remove(doc1Path)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	docsFinal, _ := db.ListDocumentPaths(col.ID)
	if len(docsFinal) != 0 {
		t.Fatalf("expected 0 docs after removal, got %d", len(docsFinal))
	}
}
