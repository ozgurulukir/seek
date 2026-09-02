package cmd_test

import (
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ozgurulukir/seek/cmd"
	"github.com/ozgurulukir/seek/internal/config"
)

func TestStatusCmd_EmptyDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db := openTestStore(t, dbPath)
	db.Close()

	cfg := &config.AppConfig{DBPath: dbPath}
	statusCmd := &cmd.StatusCmd{}
	if err := statusCmd.Run(cfg); err != nil {
		t.Fatalf("StatusCmd.Run failed on empty db: %v", err)
	}
}

func TestStatusCmd_WithCollections(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db := openTestStore(t, dbPath)

	notesDir := filepath.Join(tmpDir, "notes")
	if err := os.MkdirAll(notesDir, 0755); err != nil {
		t.Fatal(err)
	}

	col, err := db.CreateCollection("sample-notes", "markdown", notesDir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}

	docID, err := db.UpsertDocument(col.ID, filepath.Join(notesDir, "note.md"), "Note", "hash1", 10.0, 5)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.InsertChunk(docID, 0, "Chunk content", []float32{0.1, 0.2}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	cfg := &config.AppConfig{DBPath: dbPath}
	statusCmd := &cmd.StatusCmd{}
	if err := statusCmd.Run(cfg); err != nil {
		t.Fatalf("StatusCmd.Run failed: %v", err)
	}
}
