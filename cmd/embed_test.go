package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/store"
)

func TestEmbedVLTextChunks(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"output": map[string]interface{}{
				"embeddings": []map[string]interface{}{
					{"embedding": []float32{0.1, 0.2, 0.3}, "index": 0},
					{"embedding": []float32{0.4, 0.5, 0.6}, "index": 1},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tmpDir, err := os.MkdirTemp("", "seek_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	col, err := db.CreateCollection("test_col", store.CollectionTypeMarkdown, tmpDir, "*.md")
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	docID, err := db.UpsertDocument(col.ID, "doc1.md", "Doc 1", "hash1", 123.0, 10)
	if err != nil {
		t.Fatalf("failed to upsert document: %v", err)
	}

	if err := db.InsertChunk(docID, 0, "Chunk 1", nil); err != nil {
		t.Fatalf("failed to insert chunk 1: %v", err)
	}
	if err := db.InsertChunk(docID, 1, "Chunk 2", nil); err != nil {
		t.Fatalf("failed to insert chunk 2: %v", err)
	}

	chunks, err := db.GetChunksWithoutEmbedding(false)
	if err != nil {
		t.Fatalf("failed to get chunks: %v", err)
	}

	vlClient := embed.NewVLClient("test-key", "test-model", 3, ts.URL)
	cmd := &EmbedCmd{}

	updated := cmd.embedVLTextChunks(db, vlClient, chunks)
	if updated != 2 {
		t.Errorf("expected 2 chunks updated, got %d", updated)
	}

	// Verify chunks have embeddings
	remaining, err := db.GetChunksWithoutEmbedding(false)
	if err != nil {
		t.Fatalf("failed to get chunks without embedding: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 chunks without embedding, got %d", len(remaining))
	}
}

func TestEmbedVLImageChunks(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"output": map[string]interface{}{
				"embeddings": []map[string]interface{}{
					{"embedding": []float32{0.7, 0.8, 0.9}, "index": 0},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tmpDir, err := os.MkdirTemp("", "seek_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	imgPath := filepath.Join(tmpDir, "test.png")
	if err := os.WriteFile(imgPath, []byte("fake png content"), 0644); err != nil {
		t.Fatalf("failed to write test image: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	col, err := db.CreateCollection("img_col", store.CollectionTypeImages, tmpDir, "*.png")
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	docID, err := db.UpsertDocument(col.ID, "test.png", "Image 1", "hash2", 123.0, 1)
	if err != nil {
		t.Fatalf("failed to upsert document: %v", err)
	}

	if err := db.InsertImageChunk(docID, 0, "Image caption", imgPath, nil); err != nil {
		t.Fatalf("failed to insert image chunk: %v", err)
	}

	chunks, err := db.GetChunksWithoutEmbedding(false)
	if err != nil {
		t.Fatalf("failed to get chunks: %v", err)
	}

	vlClient := embed.NewVLClient("test-key", "test-model", 3, ts.URL)
	cmd := &EmbedCmd{}

	updated := cmd.embedVLImageChunks(db, vlClient, chunks)
	if updated != 1 {
		t.Errorf("expected 1 chunk updated, got %d", updated)
	}

	// Verify chunks have embeddings
	remaining, err := db.GetChunksWithoutEmbedding(false)
	if err != nil {
		t.Fatalf("failed to get chunks without embedding: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 chunks without embedding, got %d", len(remaining))
	}
}
