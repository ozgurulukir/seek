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

// writeSampleRepo creates a small repo so code/document sync has files to read.
func writeSampleRepo(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "helper.py"), []byte("def helper(): pass\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestAddCmd_TypeCode verifies the canonical --type code selector reaches the
// same store effect as the legacy --code flag.
func TestAddCmd_TypeCode(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := &config.AppConfig{
		DBPath:   dbPath,
		CacheDir: filepath.Join(tmpDir, "cache"),
	}

	repoDir := filepath.Join(tmpDir, "sample-project")
	writeSampleRepo(t, repoDir)

	addCmd := &cmd.AddCmd{Path: repoDir, Name: "sample-repo", Type: "code"}
	if err := addCmd.Run(cfg); err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags \"fts5 sqlite_fts5\" ./... or make test")
		}
		t.Fatalf("AddCmd.Run(--type code) failed: %v", err)
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
}

// TestAddCmd_TypeDocuments verifies the canonical --type documents selector
// creates a documents collection (and honors --backend).
func TestAddCmd_TypeDocuments(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := &config.AppConfig{
		DBPath:   dbPath,
		CacheDir: filepath.Join(tmpDir, "cache"),
	}

	docDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docDir, "readme.md"), []byte("# hi\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// builtin keeps the sync local (no remote xberg server required) and lets us
	// assert the per-collection backend is persisted.
	addCmd := &cmd.AddCmd{Path: docDir, Name: "docs", Type: "documents", Backend: "builtin"}
	if err := addCmd.Run(cfg); err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags \"fts5 sqlite_fts5\" ./... or make test")
		}
		t.Fatalf("AddCmd.Run(--type documents) failed: %v", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	col, err := db.GetCollectionByName("docs")
	if err != nil {
		t.Fatalf("collection not found: %v", err)
	}
	if col.Type != store.CollectionTypeDocuments {
		t.Errorf("expected collection type %q, got %q", store.CollectionTypeDocuments, col.Type)
	}
	if col.Backend != "builtin" {
		t.Errorf("expected persisted backend %q, got %q", "builtin", col.Backend)
	}
}

// TestAddCmd_ConflictErrors verifies conflicting selectors fail fast (before the
// store is opened) with a clear error, rather than silently resolving.
func TestAddCmd_ConflictErrors(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.AppConfig{
		DBPath:   filepath.Join(tmpDir, "test.db"),
		CacheDir: filepath.Join(tmpDir, "cache"),
	}

	cases := []struct {
		name   string
		add    cmd.AddCmd
		errSub string
	}{
		{"--code --pdf", cmd.AddCmd{Path: tmpDir, Code: true, Pdf: true}, "conflicting collection types"},
		{"--type code --pdf", cmd.AddCmd{Path: tmpDir, Type: "code", Pdf: true}, "conflicting collection types"},
		{"--claude --parser foo", cmd.AddCmd{Path: tmpDir, Claude: true, Parser: "foo"}, "conflicting collection types"},
		{"--opencode --copilot", cmd.AddCmd{Path: tmpDir, Opencode: true, Copilot: true}, "multiple parser sources"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.add.Run(cfg); err == nil {
				t.Fatalf("expected error containing %q, got success", tc.errSub)
			} else if !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("expected error containing %q, got: %v", tc.errSub, err)
			}
		})
	}
}
