package indexer_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/store"
)

func setupCodeIndexerTest(t *testing.T) (*indexer.Indexer, *store.Store, *store.Collection, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	cfg := &config.AppConfig{
		DBPath: dbPath,
	}

	idx := indexer.New(cfg, db).WithLogger(nopLogger{})

	codeDir := filepath.Join(tmpDir, "my-repo")
	if err := os.MkdirAll(filepath.Join(codeDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}

	col, err := db.CreateCollection("my-repo", store.CollectionTypeCode, codeDir, "**/*")
	if err != nil {
		t.Fatal(err)
	}

	return idx, db, col, codeDir
}

func TestIndexer_CodeCollectionSync(t *testing.T) {
	idx, db, col, codeDir := setupCodeIndexerTest(t)

	mainGo := filepath.Join(codeDir, "main.go")
	appTs := filepath.Join(codeDir, "src", "app.ts")

	t.Run("Initial empty sync", func(t *testing.T) {
		if err := idx.SyncCollection(col); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Add files and verify fastfields", func(t *testing.T) {
		if err := os.WriteFile(mainGo, []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(appTs, []byte("export const version = '1.0.0';\n"), 0644); err != nil {
			t.Fatal(err)
		}

		if err := idx.SyncCollection(col); err != nil {
			t.Fatal(err)
		}

		docs, err := db.ListDocumentPaths(col.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 2 {
			t.Fatalf("expected 2 docs, got %d", len(docs))
		}

		// Verify fast field metadata
		mainDocID := docs[mainGo]
		langVal, err := db.FastFields().Get(mainDocID, "lang")
		if err != nil {
			t.Fatalf("failed to get fastfield: %v", err)
		}
		if langVal != "go" {
			t.Errorf("expected lang 'go', got %v", langVal)
		}

		tsDocID := docs[appTs]
		tsLang, err := db.FastFields().Get(tsDocID, "lang")
		if err != nil {
			t.Fatalf("failed to get ts fastfield: %v", err)
		}
		if tsLang != "typescript" {
			t.Errorf("expected lang 'typescript', got %v", tsLang)
		}
	})

	t.Run("Unchanged sync", func(t *testing.T) {
		if err := idx.SyncCollection(col); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Update file", func(t *testing.T) {
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(mainGo, []byte("package main\n\nfunc main() {\n\tprintln(\"updated\")\n}\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := idx.SyncCollection(col); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Delete file and orphan cleanup", func(t *testing.T) {
		if err := os.Remove(appTs); err != nil {
			t.Fatal(err)
		}
		if err := idx.SyncCollection(col); err != nil {
			t.Fatal(err)
		}

		docsAfter, err := db.ListDocumentPaths(col.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(docsAfter) != 1 {
			t.Fatalf("expected 1 doc after delete, got %d", len(docsAfter))
		}
		if _, ok := docsAfter[mainGo]; !ok {
			t.Errorf("expected main.go to still exist")
		}
	})
}
