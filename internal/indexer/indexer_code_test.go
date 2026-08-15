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

func TestIndexer_CodeCollectionSync(t *testing.T) {
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

	codeDir := filepath.Join(tmpDir, "my-repo")
	if err := os.MkdirAll(filepath.Join(codeDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}

	col, err := db.CreateCollection("my-repo", store.CollectionTypeCode, codeDir, "**/*")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Initial sync (empty)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	// 2. Add files
	mainGo := filepath.Join(codeDir, "main.go")
	if err := os.WriteFile(mainGo, []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	appTs := filepath.Join(codeDir, "src", "app.ts")
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

	// 3. Unchanged sync
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	// 4. Update file
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(mainGo, []byte("package main\n\nfunc main() {\n\tprintln(\"updated\")\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	// 5. Delete file (orphan cleanup)
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
}
