package indexer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
)

// TestIndexer_ImageSyncMtimeBump is the regression test for the image-handler
// mtime drift: an unchanged image must be skipped AND get its stored mtime
// refreshed, matching the markdown/code/documents policy.
func TestIndexer_ImageSyncMtimeBump(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db := openTestStore(t, dbPath)
	defer db.Close()

	cfg := &config.AppConfig{DBPath: dbPath}
	idx := indexer.New(cfg, db).WithLogger(nopLogger{})

	col, err := db.CreateCollection("test-images", "images", tmpDir, "")
	if err != nil {
		t.Fatal(err)
	}

	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG magic
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1, 8-bit, palette
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "a.png"), png, 0644); err != nil {
		t.Fatal(err)
	}

	// First sync indexes the file.
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	doc, err := db.GetDocumentContext(context.Background(), col.ID, filepath.Join(tmpDir, "a.png"))
	if err != nil {
		t.Fatalf("image not indexed: %v", err)
	}
	oldMtime := doc.Mtime

	// Touch the file so its on-disk mtime moves forward, leave content the same.
	later := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(filepath.Join(tmpDir, "a.png"), later, later); err != nil {
		t.Fatal(err)
	}

	// Second sync must skip the unchanged file AND refresh the stored mtime.
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	fresh, err := db.GetDocumentContext(context.Background(), col.ID, filepath.Join(tmpDir, "a.png"))
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Mtime == oldMtime {
		t.Fatalf("unchanged image skipped without mtime bump: old=%v new=%v", oldMtime, fresh.Mtime)
	}
}
