package indexer

import (
	"context"
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
		paths, _ := db.ListDocumentPaths(col.ID)
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
