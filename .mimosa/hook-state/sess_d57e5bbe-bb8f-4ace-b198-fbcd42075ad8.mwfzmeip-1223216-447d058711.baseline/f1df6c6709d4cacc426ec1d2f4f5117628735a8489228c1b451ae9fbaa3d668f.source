package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestFTSNeedsRebuildWhenTableMissing pins the crash-healing contract: a
// missing documents_fts must trigger a rebuild, not be silently recreated
// empty. Under the old raw-BEGIN/COMMIT scheme a crash after the autocommitted
// DROP left exactly this state, and ftsNeedsRebuild reported "no rebuild
// needed" — BM25 search returned nothing forever (review 2026-09-17 M2).
func TestFTSNeedsRebuildWhenTableMissing(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.db.Exec(`DROP TABLE documents_fts`); err != nil {
		t.Fatalf("drop documents_fts: %v", err)
	}
	needRebuild, err := s.ftsNeedsRebuild()
	if err != nil {
		t.Fatalf("ftsNeedsRebuild: %v", err)
	}
	if !needRebuild {
		t.Fatal("expected ftsNeedsRebuild to be true when documents_fts is missing")
	}
}

// TestFTSRebuildHealsMissingTableOnReopen verifies end-to-end recovery:
// content indexed before documents_fts vanished is searchable again after the
// store is reopened, because initFTS repopulates the table from chunks inside
// a real transaction.
func TestFTSRebuildHealsMissingTableOnReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "heal.db")
	s, err := Open(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags \"fts5 sqlite_fts5\" ./... or make test")
		}
		t.Fatalf("Open: %v", err)
	}

	col, err := s.CreateCollection("heal", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	docID, err := s.UpsertDocument(col.ID, "/tmp/heal.md", "Heal Document", "hash", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if err := s.InsertChunk(docID, 0, "uniquetoken reconstructible content", nil); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}

	// Simulate the crash state: the FTS table is dropped without
	// repopulation, then the process (the store handle) dies.
	if _, err := s.db.Exec(`DROP TABLE documents_fts`); err != nil {
		t.Fatalf("drop documents_fts: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening must detect the missing table and repopulate it from chunks.
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { s2.Close() })

	results, err := s2.SearchFTS("uniquetoken", 5, nil)
	if err != nil {
		t.Fatalf("SearchFTS after reopen: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected healed FTS to find the chunk content, got no results")
	}
}
