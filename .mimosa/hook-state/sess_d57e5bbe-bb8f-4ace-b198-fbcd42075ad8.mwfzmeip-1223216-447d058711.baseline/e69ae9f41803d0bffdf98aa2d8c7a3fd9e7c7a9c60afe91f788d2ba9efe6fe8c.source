package cmd_test

import (
	"encoding/json"
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
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags \"fts5 sqlite_fts5\" ./... or make test")
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

func TestSyncCmd_ErrorIncludesCollectionName(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db := openTestStore(t, dbPath)

	// An unknown collection type makes SyncCollection fail deterministically
	// and synchronously, without depending on filesystem state.
	col, err := db.CreateCollection("unknown-type-col", store.CollectionType("bogus"), tmpDir, "")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	cfg := &config.AppConfig{
		DBPath:   dbPath,
		CacheDir: filepath.Join(tmpDir, "cache"),
	}

	syncCmd := &cmd.SyncCmd{}
	err = syncCmd.Run(cfg)
	if err == nil {
		t.Fatalf("expected SyncCmd.Run to fail for an unknown collection type")
	}
	if !strings.Contains(err.Error(), col.Name) {
		t.Errorf("error %q does not include the failing collection name %q", err.Error(), col.Name)
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

func TestSyncCmd_MissingOptionalSourceDoesNotHideCodeSuccess(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// os.UserHomeDir reads %USERPROFILE% on Windows, so HOME alone would leave
	// the claude scanner pointing at the developer's real ~/.claude/projects.
	t.Setenv("USERPROFILE", tmp)
	path := filepath.Join(tmp, "notes")
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "note.md"), []byte("# Indexed\nThis document must survive an unavailable source."), 0644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(tmp, "test.db")
	db := openTestStore(t, dbPath)
	col, err := db.CreateCollection("notes", "markdown", path, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateCollection("claude", "claude", "", ""); err != nil {
		t.Fatal(err)
	}
	db.Close()
	cfg := &config.AppConfig{DBPath: dbPath, CacheDir: filepath.Join(tmp, "cache")}
	var syncErr error
	output := captureStdout(t, func() { syncErr = (&cmd.SyncCmd{NoEmbed: true, JSON: true}).Run(cfg) })
	if syncErr != nil {
		t.Fatal(syncErr)
	}
	var outcomes []struct {
		Collection string `json:"collection"`
		Status     string `json:"status"`
		Failed     int    `json:"failed"`
	}
	if err := json.Unmarshal([]byte(output), &outcomes); err != nil {
		t.Fatalf("invalid JSON output: %v: %s", err, output)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes=%v", outcomes)
	}
	for _, out := range outcomes {
		if out.Collection == "claude" && (out.Status != "skipped" || out.Failed != 0) {
			t.Fatalf("outcome=%v", out)
		}
	}
	db = openTestStore(t, dbPath)
	count, err := db.CountDocuments(col.ID)
	db.Close()
	if err != nil || count != 1 {
		t.Fatalf("documents=%d err=%v", count, err)
	}
	for _, sync := range []cmd.SyncCmd{{NoEmbed: true, Strict: true}, {NoEmbed: true, Collection: "claude"}, {NoEmbed: true, Collection: "missing"}} {
		if err := sync.Run(cfg); err == nil {
			t.Fatalf("expected failure: %+v", sync)
		}
	}
}
