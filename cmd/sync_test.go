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

func openTestStore(t *testing.T, dbPath string) *store.Store {
	t.Helper()
	db, err := store.Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags fts5 ./... or make test")
		}
		t.Fatal(err)
	}
	return db
}

func TestSyncCmd_EmptyDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db := openTestStore(t, dbPath)
	db.Close()

	cfg := &config.AppConfig{
		DBPath:   dbPath,
		CacheDir: filepath.Join(tmpDir, "cache"),
	}

	syncCmd := &cmd.SyncCmd{}
	if err := syncCmd.Run(cfg); err != nil {
		t.Fatalf("SyncCmd.Run failed on empty db: %v", err)
	}
}

func TestSyncCmd_SyncSpecificCollection(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db := openTestStore(t, dbPath)

	notesDir := filepath.Join(tmpDir, "notes")
	if err := os.MkdirAll(notesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "note1.md"), []byte("# Note 1\nContent of note 1"), 0644); err != nil {
		t.Fatal(err)
	}

	otherDir := filepath.Join(tmpDir, "other")
	if err := os.MkdirAll(otherDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "doc.md"), []byte("# Other\nOther content"), 0644); err != nil {
		t.Fatal(err)
	}

	col1, err := db.CreateCollection("my-notes", "markdown", notesDir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	col2, err := db.CreateCollection("other-notes", "markdown", otherDir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	cfg := &config.AppConfig{
		DBPath:   dbPath,
		CacheDir: filepath.Join(tmpDir, "cache"),
	}

	// Sync only col1
	syncCmd := &cmd.SyncCmd{Collection: col1.Name}
	if err := syncCmd.Run(cfg); err != nil {
		t.Fatalf("SyncCmd.Run failed: %v", err)
	}

	db2 := openTestStore(t, dbPath)
	defer db2.Close()

	docs1, err := db2.CountDocuments(col1.ID)
	if err != nil || docs1 != 1 {
		t.Fatalf("expected 1 document in col1, got %d (err: %v)", docs1, err)
	}

	// col2 was not synced, should have 0 docs
	docs2, err := db2.CountDocuments(col2.ID)
	if err != nil || docs2 != 0 {
		t.Fatalf("expected 0 documents in col2, got %d (err: %v)", docs2, err)
	}
}

func TestSyncCmd_SyncAllCollections(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db := openTestStore(t, dbPath)

	notesDir := filepath.Join(tmpDir, "notes")
	if err := os.MkdirAll(notesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "a.md"), []byte("# A\nContent"), 0644); err != nil {
		t.Fatal(err)
	}

	codeDir := filepath.Join(tmpDir, "code")
	if err := os.MkdirAll(codeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codeDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	col1, _ := db.CreateCollection("notes", "markdown", notesDir, "**/*.md")
	col2, _ := db.CreateCollection("code", "code", codeDir, "**/*")
	db.Close()

	cfg := &config.AppConfig{
		DBPath:   dbPath,
		CacheDir: filepath.Join(tmpDir, "cache"),
	}

	syncCmd := &cmd.SyncCmd{} // empty collection name => all
	if err := syncCmd.Run(cfg); err != nil {
		t.Fatalf("SyncCmd.Run failed: %v", err)
	}

	db2 := openTestStore(t, dbPath)
	defer db2.Close()

	docs1, _ := db2.CountDocuments(col1.ID)
	docs2, _ := db2.CountDocuments(col2.ID)
	if docs1 != 1 || docs2 != 1 {
		t.Errorf("expected 1 doc in each, got notes=%d, code=%d", docs1, docs2)
	}
}
