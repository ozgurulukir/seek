package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/source"
	"github.com/ozgurulukir/seek/internal/store"
)

func TestConversationTitleResolvesByPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		batch    ConversationBatch
		fromLine int
		getTitle func(sessionID, defaultTitle string) string
		want     string
	}{
		{
			name: "full parse without parser title falls back to base name",
			path: "/tmp/sessions/abc.jsonl",
			want: "abc.jsonl",
		},
		{
			name:  "full parse uses parser title",
			path:  "/tmp/sessions/abc.jsonl",
			batch: ConversationBatch{Title: "Debugging the indexer"},
			want:  "Debugging the indexer",
		},
		{
			name:     "append ignores parser title because the delta carries none",
			path:     "/tmp/sessions/abc.jsonl",
			batch:    ConversationBatch{Title: "Stale title"},
			fromLine: 120,
			want:     "abc.jsonl",
		},
		{
			name:     "session-title lookup overrides on append",
			path:     "/tmp/sessions/abc.jsonl",
			batch:    ConversationBatch{SessionID: "s1"},
			fromLine: 120,
			getTitle: func(sessionID, defaultTitle string) string { return "Session " + sessionID },
			want:     "Session s1",
		},
		{
			name:     "session-title lookup also overrides a full parse",
			path:     "/tmp/sessions/abc.jsonl",
			batch:    ConversationBatch{Title: "Parser title", SessionID: "s1"},
			getTitle: func(sessionID, defaultTitle string) string { return "Session " + sessionID },
			want:     "Session s1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := conversationTitle(tt.path, tt.batch, tt.fromLine, tt.getTitle); got != tt.want {
				t.Errorf("conversationTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildConversationChunksSequencesAndOffsets(t *testing.T) {
	t.Run("full parse numbers chunks from baseSeq", func(t *testing.T) {
		chunks, nextSeq := buildConversationChunks(ConversationBatch{Text: "line one\nline two"}, 0, 0, 1024)
		if len(chunks) == 0 {
			t.Fatal("expected text chunks")
		}
		for i, c := range chunks {
			if c.Seq != i {
				t.Errorf("chunk %d Seq = %d, want %d", i, c.Seq, i)
			}
		}
		if nextSeq != len(chunks) {
			t.Errorf("nextSeq = %d, want %d", nextSeq, len(chunks))
		}
	})

	t.Run("append continues seqs and offsets line spans by fromLine", func(t *testing.T) {
		chunks, nextSeq := buildConversationChunks(ConversationBatch{Text: "delta line"}, 7, 40, 1024)
		if len(chunks) != 1 {
			t.Fatalf("got %d chunks, want 1", len(chunks))
		}
		if chunks[0].Seq != 7 {
			t.Errorf("Seq = %d, want 7", chunks[0].Seq)
		}
		if chunks[0].StartLine != 41 || chunks[0].EndLine != 41 {
			t.Errorf("line span = %d-%d, want 41-41 (1-based delta + 40 offset)", chunks[0].StartLine, chunks[0].EndLine)
		}
		if nextSeq != 8 {
			t.Errorf("nextSeq = %d, want 8", nextSeq)
		}
	})

	t.Run("images take the seqs after the text chunks", func(t *testing.T) {
		batch := ConversationBatch{
			Text: "hello",
			Images: []source.ConversationImage{
				{Context: "screenshot", SavedPath: "img/1.png"},
				{Context: "second shot", SavedPath: "img/2.png"},
			},
		}
		chunks, nextSeq := buildConversationChunks(batch, 0, 0, 1024)
		textChunks := 0
		for _, c := range chunks {
			if c.ChunkType == store.ChunkTypeText {
				textChunks++
			}
		}
		for i, c := range chunks[textChunks:] {
			if c.ChunkType != store.ChunkTypeImage {
				t.Fatalf("chunk %d after text is not an image chunk", textChunks+i)
			}
			if c.Seq != textChunks+i {
				t.Errorf("image chunk Seq = %d, want %d", c.Seq, textChunks+i)
			}
		}
		if nextSeq != len(chunks) {
			t.Errorf("nextSeq = %d, want %d", nextSeq, len(chunks))
		}
	})

	t.Run("images-only batch starts at baseSeq", func(t *testing.T) {
		batch := ConversationBatch{
			Images: []source.ConversationImage{{Context: "screenshot", SavedPath: "img/1.png"}},
		}
		chunks, nextSeq := buildConversationChunks(batch, 5, 0, 1024)
		if len(chunks) != 1 || chunks[0].Seq != 5 {
			t.Fatalf("got %+v, want one chunk with Seq 5", chunks)
		}
		if nextSeq != 6 {
			t.Errorf("nextSeq = %d, want 6", nextSeq)
		}
	})
}

func TestConversationBaseSeqContinuesAppendSequence(t *testing.T) {
	t.Run("full parse needs no prior state", func(t *testing.T) {
		idx, _, _ := cleanupTestIndexer(t)
		if got, err := idx.conversationBaseSeq(nil, 0, "x"); err != nil || got != 0 {
			t.Errorf("conversationBaseSeq(nil, 0) = %d, %v; want 0, nil", got, err)
		}
	})

	t.Run("append continues after the highest stored seq", func(t *testing.T) {
		idx, db, col := cleanupTestIndexer(t)
		path := filepath.Join(col.Path, "session.jsonl")
		if _, err := db.UpsertAndReplaceIndex(context.Background(), store.DocumentIndex{
			CollectionID: col.ID,
			Path:         path,
			Title:        path,
			Chunks: []store.IndexChunk{
				{Seq: 0, Content: "a"},
				{Seq: 1, Content: "b"},
			},
		}); err != nil {
			t.Fatal(err)
		}
		doc, err := db.GetDocumentContext(context.Background(), col.ID, path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := idx.conversationBaseSeq(doc, 10, path)
		if err != nil {
			t.Fatalf("conversationBaseSeq: %v", err)
		}
		if got != 2 {
			t.Errorf("baseSeq = %d, want 2", got)
		}
	})

	t.Run("append with missing document state is an error", func(t *testing.T) {
		idx, _, _ := cleanupTestIndexer(t)
		_, err := idx.conversationBaseSeq(nil, 10, "session.jsonl")
		if err == nil || !strings.Contains(err.Error(), "document state is missing") {
			t.Errorf("err = %v, want 'document state is missing'", err)
		}
	})
}

func TestPruneEmptyConversationBatch(t *testing.T) {
	seed := func(t *testing.T) (*Indexer, *store.Store, *store.Collection, *store.Document) {
		t.Helper()
		idx, db, col := cleanupTestIndexer(t)
		path := filepath.Join(col.Path, "session.jsonl")
		if _, err := db.UpsertAndReplaceIndex(context.Background(), store.DocumentIndex{
			CollectionID: col.ID,
			Path:         path,
			Title:        path,
			Mtime:        100,
		}); err != nil {
			t.Fatal(err)
		}
		doc, err := db.GetDocumentContext(context.Background(), col.ID, path)
		if err != nil {
			t.Fatal(err)
		}
		return idx, db, col, doc
	}

	t.Run("full re-parse with no content deletes the stale document", func(t *testing.T) {
		idx, db, col, doc := seed(t)
		if err := idx.pruneEmptyConversationBatch(doc, 0, doc.Path, 200); err != nil {
			t.Fatalf("pruneEmptyConversationBatch: %v", err)
		}
		paths, _ := db.ListDocumentPathsContext(context.Background(), col.ID)
		if len(paths) != 0 {
			t.Errorf("stale document survived, remaining: %v", paths)
		}
	})

	t.Run("append with no new content only records the mtime", func(t *testing.T) {
		idx, db, _, doc := seed(t)
		if err := idx.pruneEmptyConversationBatch(doc, 50, doc.Path, 200); err != nil {
			t.Fatalf("pruneEmptyConversationBatch: %v", err)
		}
		got, err := db.GetDocumentContext(context.Background(), doc.CollectionID, doc.Path)
		if err != nil {
			t.Fatalf("document vanished after mtime update: %v", err)
		}
		if got.Mtime != 200 {
			t.Errorf("Mtime = %v, want 200", got.Mtime)
		}
	})

	t.Run("unknown document is a no-op", func(t *testing.T) {
		idx, _, _, _ := seed(t)
		if err := idx.pruneEmptyConversationBatch(nil, 0, "ghost.jsonl", 200); err != nil {
			t.Errorf("nil existing must be a no-op, got %v", err)
		}
	})
}

// syncConversationFixture drives syncConversation with a fixed mtime and a
// caller-controlled content hash, so a test can change content while keeping
// the timestamp identical. The returned closures read the live file.
func syncConversationFixture(t *testing.T, idx *Indexer, col *store.Collection, path string) (func() ([]source.ConversationFile, []source.ScanIssue, error), func(string, int) (ConversationBatch, error), *string) {
	t.Helper()
	hash := "hash-v1"
	const mtime = 100.0
	scanFiles := func() ([]source.ConversationFile, []source.ScanIssue, error) {
		return []source.ConversationFile{{Path: path, Mtime: mtime, ContentHash: hash}}, nil, nil
	}
	parseFile := func(p string, fromLine int) (ConversationBatch, error) {
		data, err := os.ReadFile(p)
		if err != nil {
			return ConversationBatch{}, err
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		if fromLine > 0 {
			if fromLine >= len(lines) {
				return ConversationBatch{}, nil
			}
			lines = lines[fromLine:]
		}
		return ConversationBatch{SessionID: "s1", Text: strings.Join(lines, "\n")}, nil
	}
	return scanFiles, parseFile, &hash
}

// TestSyncConversationContentChangeWithUnchangedMtimeReindexes pins the fix for
// mtime-only skip gates: an in-place edit that preserves the timestamp must
// still be re-indexed because the content hash changed.
func TestSyncConversationContentChangeWithUnchangedMtimeReindexes(t *testing.T) {
	idx, db, col := cleanupTestIndexer(t)
	col.Type = store.CollectionTypeClaude

	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("alpha question\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scanFiles, parseFile, hash := syncConversationFixture(t, idx, col, path)

	if err := idx.syncConversation(col, scanFiles, parseFile, nil); err != nil {
		t.Fatal(err)
	}
	if res, err := db.SearchFTSContext(context.Background(), `"alpha question"`, 5, nil); err != nil || len(res) != 1 {
		t.Fatalf("first sync FTS alpha = %+v (%v), want 1", res, err)
	}

	// Change the content but keep the hash pointer's mtime identical (100.0).
	if err := os.WriteFile(path, []byte("beta question\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	*hash = "hash-v2"
	if err := idx.syncConversation(col, scanFiles, parseFile, nil); err != nil {
		t.Fatal(err)
	}
	if res, err := db.SearchFTSContext(context.Background(), `"beta question"`, 5, nil); err != nil || len(res) != 1 {
		t.Fatalf("FTS beta after unchanged-mtime edit = %+v (%v), want 1", res, err)
	}
	if res, err := db.SearchFTSContext(context.Background(), `"alpha question"`, 5, nil); err != nil || len(res) != 0 {
		t.Fatalf("stale alpha still indexed = %+v (%v), want 0", res, err)
	}
	doc, err := db.GetDocumentContext(context.Background(), col.ID, path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.ContentHash != "hash-v2" {
		t.Errorf("ContentHash = %q, want hash-v2", doc.ContentHash)
	}
}

// TestSyncConversationAppendCarriesSemanticFields pins the append-enrichment
// fix: appended chunks must be re-enriched over the whole conversation. The
// provider's response is swapped between passes so a stale "preserve prior
// fields" path would be observable (it would keep the old tags).
func TestSyncConversationAppendCarriesSemanticFields(t *testing.T) {
	idx, db, col := cleanupTestIndexer(t)
	col.Type = store.CollectionTypeClaude

	fake := &fakeSemanticProvider{
		healthOk: true,
		health:   semanticHealthAll(),
		response: fakeTagResult("golang", "concurrency"),
	}
	idx.WithSemanticProvider(fake)

	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("alpha question\nalpha answer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scanFiles, parseFile, hash := syncConversationFixture(t, idx, col, path)

	if err := idx.syncConversation(col, scanFiles, parseFile, nil); err != nil {
		t.Fatal(err)
	}
	doc, err := db.GetDocumentContext(context.Background(), col.ID, path)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := db.FastFields().Get(doc.ID, "tags"); err != nil || v != "golang,concurrency" {
		t.Fatalf("tags after initial sync = %v (%v), want golang,concurrency", v, err)
	}

	// Append two lines; hash changes, mtime does not. Swap the provider result
	// so a recompute on append is observable.
	if err := os.WriteFile(path, []byte("alpha question\nalpha answer\nbeta question\nbeta answer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	*hash = "hash-v2"
	fake.response = fakeTagResult("rust", "ownership")

	if err := idx.syncConversation(col, scanFiles, parseFile, nil); err != nil {
		t.Fatal(err)
	}
	doc, err = db.GetDocumentContext(context.Background(), col.ID, path)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := db.FastFields().Get(doc.ID, "tags"); err != nil || v != "rust,ownership" {
		t.Fatalf("tags after append = %v (%v), want rust,ownership (append must re-enrich)", v, err)
	}
	if doc.ContentHash != "hash-v2" {
		t.Errorf("ContentHash = %q, want hash-v2", doc.ContentHash)
	}
	if res, err := db.SearchFTSContext(context.Background(), `"beta question"`, 5, nil); err != nil || len(res) != 1 {
		t.Fatalf("appended text FTS = %+v (%v), want 1", res, err)
	}
	states, err := db.GetSemanticStates(context.Background(), []int64{doc.ID})
	if err != nil {
		t.Fatal(err)
	}
	if st := states[doc.ID]; st.Status != store.SemanticStatusCurrent || st.Fingerprint == "" {
		t.Fatalf("semantic state after append = %+v, want current with a fingerprint", st)
	}
}

// TestSyncConversationEmptyAppendRecordsHash pins the hash-skip invariant on
// the empty-delta path: an append of a line the parser ignores still changes
// the file's bytes, so its content hash must advance or every later sync
// re-parses the file for no reason.
func TestSyncConversationEmptyAppendRecordsHash(t *testing.T) {
	idx, db, col := cleanupTestIndexer(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("alpha question\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash := "hash-v1"
	const mtime = 100.0
	scanFiles := func() ([]source.ConversationFile, []source.ScanIssue, error) {
		return []source.ConversationFile{{Path: path, Mtime: mtime, ContentHash: hash}}, nil, nil
	}
	parseFile := func(p string, fromLine int) (ConversationBatch, error) {
		data, err := os.ReadFile(p)
		if err != nil {
			return ConversationBatch{}, err
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		var kept []string
		for i, line := range lines {
			if i < fromLine || strings.HasPrefix(line, "SUMMARY") {
				continue
			}
			kept = append(kept, line)
		}
		return ConversationBatch{SessionID: "s1", Text: strings.Join(kept, "\n")}, nil
	}

	if err := idx.syncConversation(col, scanFiles, parseFile, nil); err != nil {
		t.Fatal(err)
	}
	// Append a line the parser ignores; the delta is empty but the hash moves.
	if err := os.WriteFile(path, []byte("alpha question\nSUMMARY ignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash = "hash-v2"
	if err := idx.syncConversation(col, scanFiles, parseFile, nil); err != nil {
		t.Fatal(err)
	}
	doc, err := db.GetDocumentContext(context.Background(), col.ID, path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.ContentHash != "hash-v2" {
		t.Fatalf("ContentHash = %q, want hash-v2 after empty append", doc.ContentHash)
	}
	chunks, err := db.GetChunksWithoutEmbeddingContext(context.Background(), true)
	if err != nil || len(chunks) != 1 {
		t.Fatalf("chunks = %+v (%v), want 1", chunks, err)
	}
	before := chunks[0].ID
	if err := idx.syncConversation(col, scanFiles, parseFile, nil); err != nil {
		t.Fatal(err)
	}
	chunks, err = db.GetChunksWithoutEmbeddingContext(context.Background(), true)
	if err != nil || len(chunks) != 1 || chunks[0].ID != before {
		t.Fatalf("unchanged sync re-parsed the file: chunks = %+v (%v), want 1 with id %d", chunks, err, before)
	}
}
