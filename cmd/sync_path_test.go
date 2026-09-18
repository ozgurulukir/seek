package cmd_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/cmd"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

// syncPathConfig builds an AppConfig for the sync --path tests.
func syncPathConfig(t *testing.T, dbPath string) *config.AppConfig {
	t.Helper()
	return &config.AppConfig{
		DBPath:   dbPath,
		CacheDir: filepath.Join(t.TempDir(), "cache"),
	}
}

func TestSyncPath_RequiresCollectionName(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := syncPathConfig(t, dbPath)
	db := openTestStore(t, dbPath)
	db.Close()

	err := (&cmd.SyncCmd{Path: filepath.Join(tmpDir, "x.md")}).Run(cfg)
	if err == nil || !strings.Contains(err.Error(), "requires a collection name") {
		t.Fatalf("sync --path without collection = %v, want collection-required error", err)
	}
}

func TestSyncPath_RejectsTypeCombination(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := syncPathConfig(t, dbPath)
	db := openTestStore(t, dbPath)
	db.Close()

	err := (&cmd.SyncCmd{Collection: "notes", Type: "markdown", Path: filepath.Join(tmpDir, "x.md")}).Run(cfg)
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with --type") {
		t.Fatalf("sync --path with --type = %v, want combination error", err)
	}
}

func TestSyncPath_OutsideCollectionRejected(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := syncPathConfig(t, dbPath)

	db := openTestStore(t, dbPath)
	notesDir := filepath.Join(tmpDir, "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, notesDir, "**/*.md"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// A file outside the registered collection path must be rejected before
	// any index work.
	outside := filepath.Join(tmpDir, "outside.md")
	if err := os.WriteFile(outside, []byte("# outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := (&cmd.SyncCmd{Collection: "notes", Path: outside, NoEmbed: true}).Run(cfg)
	if err == nil || !strings.Contains(err.Error(), "outside collection") {
		t.Fatalf("sync --path outside = %v, want containment rejection", err)
	}
}

func TestSyncPath_InsideCollectionIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := syncPathConfig(t, dbPath)

	db := openTestStore(t, dbPath)
	notesDir := filepath.Join(tmpDir, "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(notesDir, "note.md")
	if err := os.WriteFile(md, []byte("# Note\n\nBody text of the note."), 0o644); err != nil {
		t.Fatal(err)
	}
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, notesDir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	out := captureStdout(t, func() {
		err = (&cmd.SyncCmd{Collection: "notes", Path: md, NoEmbed: true}).Run(cfg)
	})
	if err != nil {
		t.Fatalf("sync --path inside = %v (output: %s)", err, out)
	}
	// The message reports collection-wide counts and frames the path as a
	// validated guard, not the scope.
	if !strings.Contains(out, "indexed") || !strings.Contains(out, "validated inside collection") {
		t.Errorf("sync output should report collection-wide counts and the validated path:\n%s", out)
	}

	db2 := openTestStore(t, dbPath)
	defer db2.Close()
	docs, err := db2.CountDocuments(col.ID)
	if err != nil {
		t.Fatal(err)
	}
	if docs != 1 {
		t.Errorf("inside path sync indexed %d docs, want 1", docs)
	}
}

// TestSyncPath_IndexesWholeCollection pins the accepted design (reviewer
// decision (b)): `sync --path` validates containment and then runs a
// WHOLE-COLLECTION sync — the path is a security guard, not a scope filter.
// With two files in the collection, syncing --path file1 must still index
// both files, distinguishing the guarded sync from a hypothetical path-scoped
// one (which would only index file1).
func TestSyncPath_IndexesWholeCollection(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := syncPathConfig(t, dbPath)

	db := openTestStore(t, dbPath)
	notesDir := filepath.Join(tmpDir, "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	file1 := filepath.Join(notesDir, "a.md")
	file2 := filepath.Join(notesDir, "b.md")
	for _, f := range []string{file1, file2} {
		if err := os.WriteFile(f, []byte("# Body\n\nContent of the note."), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, notesDir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	err = (&cmd.SyncCmd{Collection: "notes", Path: file1, NoEmbed: true}).Run(cfg)
	if err != nil {
		t.Fatalf("sync --path on file1 = %v", err)
	}

	db2 := openTestStore(t, dbPath)
	defer db2.Close()
	docs, err := db2.CountDocuments(col.ID)
	if err != nil {
		t.Fatal(err)
	}
	if docs != 2 {
		t.Fatalf("guarded sync indexed %d docs, want 2 (whole collection, not just the validated path)", docs)
	}
	// Both files are present as documents.
	for _, want := range []string{file1, file2} {
		if _, err := db2.GetDocumentContext(context.Background(), col.ID, want); err != nil {
			t.Errorf("document %q missing after guarded sync: %v", want, err)
		}
	}
}

func TestSyncPath_RelativePathResolved(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := syncPathConfig(t, dbPath)

	db := openTestStore(t, dbPath)
	notesDir := filepath.Join(tmpDir, "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(notesDir, "note.md")
	if err := os.WriteFile(md, []byte("# Note\n\nBody text of the note."), 0o644); err != nil {
		t.Fatal(err)
	}
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, notesDir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// A relative path resolves against the process CWD; the collection
	// service must accept it as inside the collection (both are canonicalized
	// to absolute before the containment check).
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, md)
	if err != nil || filepath.IsAbs(rel) {
		// e.g. CI: temp dir and CWD live on different Windows drives, so no
		// relative path is expressible.
		t.Skip("collection dir is not under the test CWD; relative path is not expressible")
	}

	out := captureStdout(t, func() {
		err = (&cmd.SyncCmd{Collection: "notes", Path: rel, NoEmbed: true}).Run(cfg)
	})
	if err != nil {
		t.Fatalf("sync --path relative = %v (output: %s)", err, out)
	}

	db2 := openTestStore(t, dbPath)
	defer db2.Close()
	docs, err := db2.CountDocuments(col.ID)
	if err != nil {
		t.Fatal(err)
	}
	if docs != 1 {
		t.Errorf("relative path sync indexed %d docs, want 1", docs)
	}
}
