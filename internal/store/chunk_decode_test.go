package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
)

// The chunk-read paths must agree on one decode policy: a corrupt zstd blob
// falls back to the raw content column when it survived, and fails loudly
// only when both are unusable. This test pins the policy at GetChunkContent,
// the read API users reach, which goes through the shared decodeChunkText
// seam.
func TestChunkDecodeCorruptBlobPolicy(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags \"fts5 sqlite_fts5\" ./... or make test")
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	col, err := s.CreateCollection("notes", CollectionTypeMarkdown, t.TempDir(), "*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
		CollectionID: col.ID,
		Path:         "note.md",
		Title:        "Note",
		Mtime:        1,
		FTSContent:   "body text",
		Chunks:       []IndexChunk{{Seq: 0, Content: "body text", ChunkType: ChunkTypeText}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var chunkID int64
	if err := s.db.QueryRow(`SELECT id FROM chunks WHERE document_id = ?`, docID).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}

	// Corrupt the stored zstd blob directly.
	if _, err := s.db.Exec(`UPDATE chunks SET content_zstd = ? WHERE document_id = ?`, []byte{0x28, 0xB5, 0x2F, 0xFD, 0x00, 0x00}, docID); err != nil {
		t.Fatal(err)
	}

	t.Run("corrupt blob with valid raw content falls back to raw", func(t *testing.T) {
		got, err := s.GetChunkContentContext(ctx, chunkID)
		if err != nil {
			t.Fatalf("GetChunkContent: %v", err)
		}
		if got != "body text" {
			t.Fatalf("content = %q, want raw fallback %q", got, "body text")
		}
	})

	t.Run("corrupt blob with empty raw content fails loudly", func(t *testing.T) {
		if _, err := s.db.Exec(`UPDATE chunks SET content = '' WHERE document_id = ?`, docID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetChunkContentContext(ctx, chunkID); err == nil {
			t.Fatal("expected an error when both zstd and raw content are unusable")
		}
	})

	t.Run("valid blob decompresses", func(t *testing.T) {
		compressed, err := CompressString("fresh text", config.DefaultCompressionLevel)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`UPDATE chunks SET content = 'fresh text', content_zstd = ? WHERE document_id = ?`, compressed, docID); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetChunkContentContext(ctx, chunkID)
		if err != nil {
			t.Fatalf("GetChunkContent: %v", err)
		}
		if got != "fresh text" {
			t.Fatalf("content = %q, want decompressed %q", got, "fresh text")
		}
	})
}
