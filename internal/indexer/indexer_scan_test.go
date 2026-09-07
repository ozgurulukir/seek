package indexer_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/store"
)

type bufferLogger struct{ bytes.Buffer }

func (l *bufferLogger) Printf(format string, v ...interface{}) {
	fmt.Fprintf(&l.Buffer, format, v...)
}

func TestSyncMarkdownScanIssuePreservesExistingDocuments(t *testing.T) {
	tmpDir := t.TempDir()
	db := openTestStore(t, filepath.Join(tmpDir, "seek.db"))
	defer db.Close()

	path := filepath.Join(tmpDir, "note.md")
	if err := os.WriteFile(path, []byte("# Note\nindexed content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmpDir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	idx := indexer.New(&config.AppConfig{}, db).WithLogger(nopLogger{})
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(tmpDir, "missing.md"), path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	logs := &bufferLogger{}
	idx.WithLogger(logs)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	docs, err := db.ListDocumentPaths(col.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := docs[path]; !ok {
		t.Fatal("scan issue removed the existing document")
	}
	if got := logs.String(); !strings.Contains(got, "WARN: scan "+path) || !strings.Contains(got, "1 failed") {
		t.Fatalf("scan issue was not reported in sync summary: %q", got)
	}
}
