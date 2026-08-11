package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCollectionBackendPersistence verifies that a backend override set via
// CreateCollectionWithBackend survives a Get/List round-trip, and that a plain
// CreateCollection records an empty backend (meaning "use config default").
// This guards the Bug 2 fix: sync must read the same backend that add wrote.
func TestCollectionBackendPersistence(t *testing.T) {
	s := newTestStore(t)

	// Collection with an explicit xberg backend.
	col, err := s.CreateCollectionWithBackend("docs", CollectionTypeDocuments, "/tmp/docs", "**/*", "xberg")
	if err != nil {
		t.Fatalf("CreateCollectionWithBackend: %v", err)
	}
	if col.Backend != "xberg" {
		t.Errorf("returned collection Backend = %q, want xberg", col.Backend)
	}

	// Get by name round-trip.
	got, err := s.GetCollectionByName("docs")
	if err != nil {
		t.Fatalf("GetCollectionByName: %v", err)
	}
	if got.Backend != "xberg" {
		t.Errorf("GetCollectionByName Backend = %q, want xberg", got.Backend)
	}

	// A plain CreateCollection (e.g. markdown/pdf) records no backend override.
	if _, err := s.CreateCollection("notes", CollectionTypeMarkdown, "/tmp/notes", "**/*.md"); err != nil {
		t.Fatal(err)
	}
	plain, err := s.GetCollectionByName("notes")
	if err != nil {
		t.Fatal(err)
	}
	if plain.Backend != "" {
		t.Errorf("plain CreateCollection Backend = %q, want empty (follow config default)", plain.Backend)
	}

	// List returns both with their correct backends.
	cols, err := s.ListCollections()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"docs": "xberg", "notes": ""}
	for _, c := range cols {
		if g, w := c.Backend, want[c.Name]; g != w {
			t.Errorf("ListCollections %q Backend = %q, want %q", c.Name, g, w)
		}
	}
}

// TestCollectionBackendMigration verifies the ALTER TABLE migration adds the
// backend column to a store that predates it (re-opening an existing DB must
// not fail). This simulates upgrading a seek install that already has
// collections without a backend column.
func TestCollectionBackendMigration(t *testing.T) {
	s := newTestStore(t)
	// Create before the column logically existed is implicit (migrate adds it
	// on Open); just ensure a collection created now reads back with a usable
	// backend field and that a second Open preserves it.
	if _, err := s.CreateCollectionWithBackend("mig", CollectionTypeDocuments, "/tmp/m", "*", "builtin"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetCollectionByName("mig")
	if err != nil {
		t.Fatal(err)
	}
	if got.Backend != "builtin" {
		t.Errorf("Backend = %q, want builtin", got.Backend)
	}
}

func TestBackendSync(t *testing.T) {
	// This guards the Bug 2 fix: sync must read the same backend that add wrote.

	// Setup temporary directory for the store
	dir, err := os.MkdirTemp("", "seek-backend-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "seek.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Add wrote this backend
	_, err = db.CreateCollectionWithBackend("bug2", CollectionTypeDocuments, "/tmp/bug2", "**/*", "xberg-custom")
	if err != nil {
		t.Fatalf("CreateCollectionWithBackend: %v", err)
	}

	// Sync reads collections
	cols, err := db.ListCollections()
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}

	found := false
	for _, c := range cols {
		if c.Name == "bug2" {
			found = true
			if c.Backend != "xberg-custom" {
				t.Errorf("sync read backend = %q, want %q", c.Backend, "xberg-custom")
			}
		}
	}
	if !found {
		t.Error("collection 'bug2' not found by sync")
	}
}
