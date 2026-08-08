package indexer_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/store"
)

type nopLogger struct{}

func (nopLogger) Printf(format string, v ...interface{}) {}

func TestIndexer_IncrementalSync(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &config.AppConfig{
		DBPath: dbPath,
	}

	idx := indexer.New(cfg, db).WithLogger(nopLogger{})

	col, err := db.CreateCollection("test-notes", "markdown", tmpDir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Initial sync (empty)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	// 2. Add a file
	doc1Path := filepath.Join(tmpDir, "doc1.md")
	os.WriteFile(doc1Path, []byte("# Title\n\nSome content here."), 0644)

	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	docs, _ := db.ListDocumentPaths(col.ID)
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}

	_ = docs[doc1Path]
	chunks, _ := db.GetChunksWithoutEmbedding(true)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	// 3. Unchanged sync (should skip)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	chunks2, _ := db.GetChunksWithoutEmbedding(true)
	if len(chunks2) != 1 {
		t.Fatalf("expected chunk count to remain 1, got %d", len(chunks2))
	}

	// 4. Update file
	time.Sleep(10 * time.Millisecond) // ensure mtime changes
	os.WriteFile(doc1Path, []byte("# Title\n\nUpdated content."), 0644)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	docsAfter, _ := db.ListDocumentPaths(col.ID)
	if len(docsAfter) != 1 {
		t.Fatalf("expected 1 doc after update, got %d", len(docsAfter))
	}
	chunks3, _ := db.GetChunksWithoutEmbedding(true)
	if len(chunks3) != 1 {
		t.Fatalf("expected 1 chunk after update, got %d", len(chunks3))
	}
	if chunks3[0].Content != "# Title\n\nUpdated content." {
		t.Fatalf("expected updated chunk content, got %q", chunks3[0].Content)
	}

	// 5. Remove file
	os.Remove(doc1Path)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	docsFinal, _ := db.ListDocumentPaths(col.ID)
	if len(docsFinal) != 0 {
		t.Fatalf("expected 0 docs after removal, got %d", len(docsFinal))
	}
}

// createFixtureExternalDB creates an opencode-like external SQLite DB with
// sessions and messages. The cursor uses epoch_ms with sub-second precision
// (milliseconds), which is the critical case for incremental sync correctness.
func createFixtureExternalDB(t *testing.T, path string) {
	t.Helper()
	os.Remove(path)
	extDB, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open external: %v", err)
	}
	defer extDB.Close()

	for _, stmt := range []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT, directory TEXT, parent_id TEXT, time_updated INTEGER)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, data TEXT)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, time_created INTEGER, data TEXT)`,
	} {
		if _, err := extDB.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}

	// Use a millisecond timestamp with non-zero sub-second component.
	// 1786216319613 ms → 1786216319.613 s — the .613 fraction is the key.
	ts := int64(1786216319613)
	extDB.Exec(`INSERT INTO session (id, title, directory, time_updated) VALUES (?, ?, ?, ?)`,
		"sess-a", "Alpha", "/project/a", ts)
	extDB.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?, ?, ?, ?)`,
		"m1", "sess-a", ts, `{"role":"user"}`)
	extDB.Exec(`INSERT INTO part (id, message_id, session_id, time_created, data) VALUES (?, ?, ?, ?, ?)`,
		"p1", "m1", "sess-a", ts, `{"type":"text","text":"hello world"}`)
}

// TestIndexer_ParserCollectionSync exercises the full syncParserDef path:
// add → full sync → incremental sync (must skip unchanged sessions, not re-index).
// This is the regression test for the sub-second cursor truncation bug.
func TestIndexer_ParserCollectionSync(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create a fixture external DB.
	extPath := filepath.Join(tmpDir, "external.db")
	createFixtureExternalDB(t, extPath)

	// Write a user override schema pointing at the fixture.
	// We use HOME override so parserdef.Load finds it.
	overrideDir := filepath.Join(tmpDir, ".config", "seek", "parsers")
	os.MkdirAll(overrideDir, 0755)
	t.Setenv("HOME", tmpDir)
	schemaContent := fmt.Sprintf(`
format: 1
name: test-fixture
description: test
sources:
  - driver: sqlite
    paths: ["%s"]
    versions:
      - version: 1
        sessions:
          query: SELECT id, title, directory, parent_id, time_updated FROM session
          id: id
          title: title
          cursor: time_updated
          cursor_format: epoch_ms
          metadata:
            workspace: directory
        messages:
          query: |
            SELECT json_extract(m.data,'$.role') AS role,
                   json_extract(p.data,'$.text') AS content
            FROM message m JOIN part p ON p.message_id = m.id
            WHERE m.session_id = :session_id
            ORDER BY m.time_created
          role: role
          content: content
`, extPath)
	os.WriteFile(filepath.Join(overrideDir, "test-fixture.yaml"), []byte(schemaContent), 0644)

	cfg := &config.AppConfig{DBPath: dbPath}
	idx := indexer.New(cfg, db).WithLogger(nopLogger{})

	// Create a parser collection.
	col, err := db.CreateParserCollection("test-fixture-convs", extPath, "*", "test-fixture")
	if err != nil {
		t.Fatalf("CreateParserCollection: %v", err)
	}

	// 1. Full sync.
	if err := idx.SyncCollection(col); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	docs, _ := db.ListDocumentPaths(col.ID)
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc after full sync, got %d", len(docs))
	}

	// Verify the document has a chunk.
	chunks, _ := db.GetChunksWithoutEmbedding(true)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	firstChunkContent := chunks[0].Content

	// 2. Incremental sync — session cursor hasn't changed, so it must be skipped.
	// The bug would re-index it (because .Unix() truncates the .613 ms fraction,
	// making `since` < cursor). The fix uses .UnixMilli() so the round-trip is exact.
	// We verify by checking the chunk content is NOT re-created.
	// To detect re-indexing, we delete the existing chunk and sync again.
	// If incremental works correctly, the session is unchanged (Messages nil),
	// so no new chunk is created. If the bug is present, the session is re-indexed
	// and a new chunk appears.
	for _, docID := range docs {
		db.DeleteChunksForDocument(docID)
	}
	chunksAfterDelete, _ := db.GetChunksWithoutEmbedding(true)
	if len(chunksAfterDelete) != 0 {
		t.Fatalf("expected 0 chunks after delete, got %d", len(chunksAfterDelete))
	}

	if err := idx.SyncCollection(col); err != nil {
		t.Fatalf("incremental sync: %v", err)
	}

	// If incremental sync correctly skips the unchanged session, NO new chunks
	// should appear (the session was returned with Messages=nil, so the indexer
	// skipped it). If the cursor truncation bug is present, the session would be
	// re-indexed and chunks would reappear.
	chunksFinal, _ := db.GetChunksWithoutEmbedding(true)
	if len(chunksFinal) != 0 {
		t.Errorf("incremental sync re-indexed unchanged session: %d chunks appeared "+
			"(cursor sub-second precision lost in round-trip). Chunk content: %q",
			len(chunksFinal), firstChunkContent)
	}
}
