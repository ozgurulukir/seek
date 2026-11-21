package indexer

import (
	"path/filepath"
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
	"github.com/ozgurulukir/seek/internal/store/storetest"
)

func skipTestIndexer(t *testing.T) (*Indexer, *store.Store, *store.Collection) {
	t.Helper()
	tmp := t.TempDir()
	db, err := store.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		storetest.SkipIfFTS5Disabled(t, err)
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	col, err := db.CreateCollection("skip", store.CollectionTypeDocuments, tmp, "")
	if err != nil {
		t.Fatal(err)
	}
	idx := New(cfgFromTest(t, tmp, filepath.Join(tmp, "test.db")), db)
	idx.WithLogger(nopLogger{})
	return idx, db, col
}

func TestSkipUnchangedMissingDocument(t *testing.T) {
	idx, _, col := skipTestIndexer(t)
	unchanged, err := idx.skipUnchanged(col, "doc.md", "hash1", 100)
	if err != nil {
		t.Fatalf("missing document must not error: %v", err)
	}
	if unchanged {
		t.Error("missing document must be re-indexed")
	}
}

func TestSkipUnchangedHashMismatch(t *testing.T) {
	idx, db, col := skipTestIndexer(t)
	if _, err := db.UpsertDocument(col.ID, "doc.md", "t", "hash1", 100, 1); err != nil {
		t.Fatal(err)
	}
	unchanged, err := idx.skipUnchanged(col, "doc.md", "hash2", 100)
	if err != nil {
		t.Fatalf("hash mismatch must not error: %v", err)
	}
	if unchanged {
		t.Error("changed content must be re-indexed")
	}
}

func TestSkipUnchangedRefreshesMtime(t *testing.T) {
	idx, db, col := skipTestIndexer(t)
	docID, err := db.UpsertDocument(col.ID, "doc.md", "t", "hash1", 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := idx.skipUnchanged(col, "doc.md", "hash1", 200)
	if err != nil {
		t.Fatalf("unchanged document must not error: %v", err)
	}
	if !unchanged {
		t.Fatal("unchanged document must be skipped")
	}
	doc, err := db.GetDocumentByIDContext(idx.ctx(), docID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Mtime != 200 {
		t.Errorf("mtime not refreshed: got %v, want 200", doc.Mtime)
	}
}
