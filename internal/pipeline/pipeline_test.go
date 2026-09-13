package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
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
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags \"fts5 sqlite_fts5\" ./... or make test")
		}
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

	vlClient := embed.NewVLClient("test-key", "test-model", 3, ts.URL, embed.TaskPrefix{})
	updated, err := embedVLText(db, vlClient, chunks, testLogger{})
	if err != nil {
		t.Fatalf("embed VL text: %v", err)
	}
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
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags \"fts5 sqlite_fts5\" ./... or make test")
		}
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

	vlClient := embed.NewVLClient("test-key", "test-model", 3, ts.URL, embed.TaskPrefix{})
	updated, err := embedVLImages(db, vlClient, chunks, testLogger{})
	if err != nil {
		t.Fatalf("embed VL images: %v", err)
	}
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

type testLogger struct{}

func (testLogger) Printf(format string, args ...any) {
	fmt.Printf(format, args...)
}

func TestSelectMode(t *testing.T) {
	hosted := embed.Capabilities{RealtimeEmbeddings: true, AsyncBatch: true}
	local := embed.Capabilities{RealtimeEmbeddings: true, AsyncBatch: false}

	tests := []struct {
		name     string
		cfgMode  string
		realtime bool
		batch    bool
		caps     embed.Capabilities
		wantMode Mode
		wantErr  bool
	}{
		{name: "auto defaults to realtime", cfgMode: "", caps: local, wantMode: ModeRealtime},
		{name: "auto explicit", cfgMode: "auto", caps: local, wantMode: ModeRealtime},
		{name: "config realtime", cfgMode: "realtime", caps: local, wantMode: ModeRealtime},
		{name: "config batch hosted", cfgMode: "batch", caps: hosted, wantMode: ModeBatch},
		{name: "config batch unsupported", cfgMode: "batch", caps: local, wantErr: true},
		{name: "flag realtime overrides batch config", cfgMode: "batch", realtime: true, caps: hosted, wantMode: ModeRealtime},
		{name: "flag batch overrides realtime config", cfgMode: "realtime", batch: true, caps: hosted, wantMode: ModeBatch},
		{name: "flag batch unsupported", batch: true, caps: local, wantErr: true},
		{name: "conflicting flags", realtime: true, batch: true, caps: hosted, wantErr: true},
		{name: "invalid config mode", cfgMode: "async", caps: hosted, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := SelectMode(tt.cfgMode, tt.realtime, tt.batch, tt.caps)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("SelectMode = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectMode: %v", err)
			}
			if got != tt.wantMode {
				t.Errorf("SelectMode = %q, want %q", got, tt.wantMode)
			}
		})
	}
}

// TestEmbedPending_HTTPContract verifies the plan §5 HTTP contract: a fake
// Ollama-like server exposing only /embeddings lets a plain `seek embed`
// succeed, while `--batch` fails early with zero /files calls.
func TestEmbedPending_HTTPContract(t *testing.T) {
	var filesCalls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files" {
			filesCalls.Add(1)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]interface{}, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]interface{}{"embedding": []float32{0.1, 0.2, 0.3}, "index": i}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}))
	defer ts.Close()

	cfg := &config.AppConfig{Config: config.Config{}}
	cfg.Config.Embedding.BaseURL = ts.URL
	cfg.Config.Embedding.Model = "nomic-embed-text"
	cfg.Config.Embedding.APIKey = "sk"
	cfg.Config.Embedding.Dimensions = 3

	db := newPipelineStore(t)
	col, err := db.CreateCollection("c", store.CollectionTypeMarkdown, t.TempDir(), "*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	docID, err := db.UpsertDocument(col.ID, "doc.md", "Doc", "h", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if err := db.InsertChunk(docID, 0, "hello world", nil); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}

	// --batch on a provider without async batch support must fail before any
	// /files request is made.
	err = EmbedPendingContext(context.Background(), cfg, db, Options{Batch: true, VectorIndex: false}, testLogger{})
	if err == nil {
		t.Fatal("--batch on unsupported provider should error")
	}
	if !strings.Contains(err.Error(), "async provider batch") {
		t.Errorf("--batch error should explain async batch is unsupported, got: %v", err)
	}
	if got := filesCalls.Load(); got != 0 {
		t.Errorf("--batch made %d /files calls, want 0", got)
	}

	// Plain `seek embed` (auto -> realtime) succeeds against /embeddings.
	if err := EmbedPendingContext(context.Background(), cfg, db, Options{VectorIndex: false}, testLogger{}); err != nil {
		t.Fatalf("plain embed should succeed: %v", err)
	}
	remaining, err := db.GetChunksWithoutEmbedding(false)
	if err != nil {
		t.Fatalf("GetChunksWithoutEmbedding: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected all chunks embedded, got %d pending", len(remaining))
	}
	if got := filesCalls.Load(); got != 0 {
		t.Errorf("realtime embed made %d /files calls, want 0", got)
	}
}

// TestEmbedPending_Privacy verifies the plan §5 privacy contract: offline_only
// + numeric loopback realtime succeeds, while a remote endpoint is refused in
// all modes (keyword-only degradation, never a network call).
func TestEmbedPending_Privacy(t *testing.T) {
	// offline_only + numeric loopback: capability present, realtime succeeds.
	loopback := &config.AppConfig{Config: config.Config{}}
	loopback.Config.Privacy.OfflineOnly = true
	loopback.Config.Embedding.BaseURL = "http://127.0.0.1:11434/v1"
	loopback.Config.Embedding.Model = "nomic-embed-text"
	loopback.Config.Embedding.APIKey = "sk"
	if ok, why := embed.EmbeddingCapability(loopback); !ok {
		t.Fatalf("offline_only + loopback should be usable, got: %s", why)
	}

	// offline_only + remote: refused in every mode.
	remote := &config.AppConfig{Config: config.Config{}}
	remote.Config.Privacy.OfflineOnly = true
	remote.Config.Embedding.BaseURL = "https://api.openai.com/v1"
	remote.Config.Embedding.Model = "text-embedding-3-small"
	remote.Config.Embedding.APIKey = "sk"
	if ok, why := embed.EmbeddingCapability(remote); ok {
		t.Fatal("offline_only + remote should be refused")
	} else if !strings.Contains(why, "offline_only") {
		t.Errorf("refusal reason should mention offline_only, got: %s", why)
	}
}
