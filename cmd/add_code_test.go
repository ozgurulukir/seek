package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ozgurulukir/seek/cmd"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

func TestAddCmd_CodeCollection(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := &config.AppConfig{
		DBPath:   dbPath,
		CacheDir: filepath.Join(tmpDir, "cache"),
	}

	repoDir := filepath.Join(tmpDir, "sample-project")
	if err := os.MkdirAll(filepath.Join(repoDir, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}

	// Add sample files
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "pkg", "helper.py"), []byte("def helper(): pass\n"), 0644); err != nil {
		t.Fatal(err)
	}

	addCmd := &cmd.AddCmd{
		Path: repoDir,
		Name: "sample-repo",
		Code: true,
	}

	if err := addCmd.Run(cfg); err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags fts5 ./... or make test")
		}
		t.Fatalf("AddCmd.Run failed: %v", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	col, err := db.GetCollectionByName("sample-repo")
	if err != nil {
		t.Fatalf("collection not found: %v", err)
	}
	if col.Type != store.CollectionTypeCode {
		t.Errorf("expected collection type %q, got %q", store.CollectionTypeCode, col.Type)
	}

	docs, err := db.ListDocumentPaths(col.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents indexed, got %d", len(docs))
	}
}
