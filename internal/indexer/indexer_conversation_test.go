package indexer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/store"
)

// claudeConvLine builds a Claude Code JSONL user-message line.
func claudeConvLine(content string) string {
	// Content is short ASCII in all tests, so a plain JSON string embed is safe.
	return `{"type":"user","message":{"role":"user","content":"` + content + `"}}`
}

// bumpMtime advances the file's mtime past the current time so the next sync
// re-picks the file (sync skips when stored mtime >= file mtime).
func bumpMtime(t *testing.T, path string) {
	t.Helper()
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
}

// useFakeHome points HOME at a temp dir so the claude scanner
// (source.ScanClaudeFiles, which walks ~/.claude/projects) sees the test
// directory instead of the developer's real projects. It returns the
// projects dir, where conversation fixtures must be written.
func useFakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	projects := filepath.Join(home, ".claude", "projects")
	if err := os.MkdirAll(projects, 0755); err != nil {
		t.Fatal(err)
	}
	return projects
}

// TestSyncClaudeAppendContinuesSeq is a regression test for incremental
// conversation sync: when a conversation grows between syncs, the new chunks
// must continue after the previous sync's seqs (not restart at 0) and their
// line spans must be offset into the full file. Before the fix, appended
// chunks collided on seq 0 and GetSurroundingContext returned jumbled content
// with wrong line numbers for every conversation that spanned multiple syncs.
func TestSyncClaudeAppendContinuesSeq(t *testing.T) {
	tmpDir := t.TempDir()
	db := openTestStore(t, filepath.Join(tmpDir, "test.db"))
	defer db.Close()

	idx := indexer.New(&config.AppConfig{DBPath: filepath.Join(tmpDir, "test.db")}, db).WithLogger(nopLogger{})
	projectsDir := useFakeHome(t)

	col, err := db.CreateCollection("test-claude", "claude", tmpDir, "**/*.jsonl")
	if err != nil {
		t.Fatal(err)
	}

	convPath := filepath.Join(projectsDir, "conv1.jsonl")
	lines := []string{
		claudeConvLine("alpha question one"),
		claudeConvLine("beta question two"),
	}
	if err := os.WriteFile(convPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	chunks, err := db.GetChunksWithoutEmbedding(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk after first sync, got %d", len(chunks))
	}
	first := chunks[0]
	if first.Seq != 0 {
		t.Fatalf("first chunk seq = %d, want 0", first.Seq)
	}
	if !strings.Contains(first.Content, "alpha question one") {
		t.Fatalf("first chunk content = %q, want it to contain %q", first.Content, "alpha question one")
	}

	// Append one more line to the conversation and bump mtime.
	lines = append(lines, claudeConvLine("gamma question three"))
	if err := os.WriteFile(convPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	bumpMtime(t, convPath)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	// FTS must contain the full conversation exactly once (no duplication,
	// no loss) — AppendFTS only fires on the append path.
	res, err := db.SearchFTS(`"alpha question one"`, 5, nil)
	if err != nil {
		t.Fatalf("SearchFTS alpha: %v", err)
	}
	if len(res) != 1 || res[0].DocumentID != first.DocumentID {
		t.Fatalf("SearchFTS alpha results = %+v, want exactly the synced doc", res)
	}
	if res, err = db.SearchFTS(`"gamma question three"`, 5, nil); err != nil {
		t.Fatalf("SearchFTS gamma: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("SearchFTS gamma results = %+v, want 1", res)
	}

	chunks, err = db.GetChunksWithoutEmbedding(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks after append (original + appended), got %d", len(chunks))
	}
	// Seqs must be unique: the pre-fix bug inserted the appended chunk at
	// seq 0, colliding with the original.
	if chunks[0].Seq == chunks[1].Seq {
		t.Fatalf("seq collision after append: both chunks have seq %d", chunks[0].Seq)
	}
	var combined string
	for _, c := range chunks {
		combined += c.Content
	}
	for _, want := range []string{"alpha question one", "beta question two", "gamma question three"} {
		if !strings.Contains(combined, want) {
			t.Errorf("chunk content missing %q:\n%s", want, combined)
		}
	}

	// GetSurroundingContext for seq 0, radius 5: only the single chunk should
	// be returned with a sane line span covering the appended line (line 3 of
	// the file). The pre-fix offset bug recorded the appended text at line 1
	// (relative to the delta), collapsing the span.
	ctx, startLine, endLine, err := db.GetSurroundingContext(first.DocumentID, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx, "gamma question three") {
		t.Fatalf("surrounding context missing appended text:\n%s", ctx)
	}
	if startLine != 1 {
		t.Errorf("surrounding context startLine = %d, want 1", startLine)
	}
	if endLine < 3 {
		t.Errorf("surrounding context endLine = %d, want >= 3 (the appended line)", endLine)
	}

	// A second append must keep growing the seq sequence, never collide.
	lines = append(lines, claudeConvLine("delta question four"))
	if err := os.WriteFile(convPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	bumpMtime(t, convPath)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	chunks, err = db.GetChunksWithoutEmbedding(true)
	if err != nil {
		t.Fatal(err)
	}
	// Seqs must stay unique across the third sync pass as well.
	seqs := make(map[int]bool, len(chunks))
	combined = ""
	for _, c := range chunks {
		if seqs[c.Seq] {
			t.Fatalf("seq collision after second append: seq %d appears twice", c.Seq)
		}
		seqs[c.Seq] = true
		combined += c.Content
	}
	if !strings.Contains(combined, "delta question four") {
		t.Errorf("chunk content missing %q after second append", "delta question four")
	}
}

// TestSyncClaudeTruncatedFileResyncs is a regression test for truncated or
// in-place edited conversation files: the old sync logic treated any file
// whose line count did not grow as "unchanged", recorded the new mtime, and
// kept indexing the stale (larger) content forever. After the fix, a
// shrinking file is fully re-parsed, replacing FTS and chunks.
func TestSyncClaudeTruncatedFileResyncs(t *testing.T) {
	tmpDir := t.TempDir()
	db := openTestStore(t, filepath.Join(tmpDir, "test.db"))
	defer db.Close()

	idx := indexer.New(&config.AppConfig{DBPath: filepath.Join(tmpDir, "test.db")}, db).WithLogger(nopLogger{})
	projectsDir := useFakeHome(t)

	col, err := db.CreateCollection("test-claude", "claude", tmpDir, "**/*.jsonl")
	if err != nil {
		t.Fatal(err)
	}

	convPath := filepath.Join(projectsDir, "conv1.jsonl")
	lines := []string{
		claudeConvLine("alpha question one"),
		claudeConvLine("beta question two"),
		claudeConvLine("gamma question three"),
	}
	if err := os.WriteFile(convPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	// Truncate the file: drop the last message, keep the first two.
	truncated := lines[:2]
	if err := os.WriteFile(convPath, []byte(strings.Join(truncated, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	bumpMtime(t, convPath)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	// The truncated line must be gone from the index.
	res, err := db.SearchFTS(`"gamma question three"`, 5, nil)
	if err != nil {
		t.Fatalf("SearchFTS gamma: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("truncated content still indexed after resync: %+v", res)
	}

	// The remaining lines must still be present (no data loss).
	for _, term := range []string{`"alpha question one"`, `"beta question two"`} {
		if res, err := db.SearchFTS(term, 5, nil); err != nil {
			t.Fatalf("SearchFTS %s: %v", term, err)
		} else if len(res) != 1 {
			t.Fatalf("expected 1 result for %s after resync, got %d", term, len(res))
		}
	}

	chunks, err := db.GetChunksWithoutEmbedding(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk after resync, got %d", len(chunks))
	}
	if strings.Contains(chunks[0].Content, "gamma question three") {
		t.Errorf("chunk still contains truncated content:\n%s", chunks[0].Content)
	}

	// The document row's line count must reflect the new on-disk size.
	doc, err := db.GetDocument(col.ID, convPath)
	if err != nil {
		t.Fatal(err)
	}
	if doc.LineCount != 2 {
		t.Errorf("document line_count = %d, want 2", doc.LineCount)
	}

	// An unchanged re-sync must not re-index (mtime recorded, chunk stable).
	before := chunks[0].ID
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	chunks, err = db.GetChunksWithoutEmbedding(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0].ID != before {
		t.Fatalf("chunk was recreated on unchanged sync: got %d chunk(s), first id %d (want 1, id %d)",
			len(chunks), firstChunkID(chunks), before)
	}
}

// TestSyncClaudeEmptyFileRemovesDoc covers the degenerate truncation: a
// conversation file whose parseable content is entirely gone. The stale
// document (and its FTS entry/chunks) must be removed, not kept.
func TestSyncClaudeEmptyFileRemovesDoc(t *testing.T) {
	tmpDir := t.TempDir()
	db := openTestStore(t, filepath.Join(tmpDir, "test.db"))
	defer db.Close()

	idx := indexer.New(&config.AppConfig{DBPath: filepath.Join(tmpDir, "test.db")}, db).WithLogger(nopLogger{})
	projectsDir := useFakeHome(t)

	col, err := db.CreateCollection("test-claude", "claude", tmpDir, "**/*.jsonl")
	if err != nil {
		t.Fatal(err)
	}

	convPath := filepath.Join(projectsDir, "conv1.jsonl")
	lines := []string{
		claudeConvLine("alpha question one"),
		claudeConvLine("beta question two"),
	}
	if err := os.WriteFile(convPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	// Replace the file with a single non-message line (e.g. a session-meta
	// line the conversation parser ignores).
	if err := os.WriteFile(convPath, []byte(`{"type":"summary","summary":"no content"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	bumpMtime(t, convPath)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	docs, err := db.ListDocumentPaths(col.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected 0 documents after content emptied, got %d: %v", len(docs), docs)
	}
	if res, err := db.SearchFTS(`"alpha question one"`, 5, nil); err != nil {
		t.Fatalf("SearchFTS: %v", err)
	} else if len(res) != 0 {
		t.Fatalf("FTS entry survived document removal: %+v", res)
	}
}

func firstChunkID(chunks []store.Chunk) int64 {
	if len(chunks) == 0 {
		return -1
	}
	return chunks[0].ID
}
