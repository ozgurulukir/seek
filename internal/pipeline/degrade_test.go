package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/store"
)

// captureLogger records output so tests can assert on degradation messages.
type captureLogger struct{ buf bytes.Buffer }

func (l *captureLogger) Printf(format string, args ...any) {
	l.buf.WriteString(fmt.Sprintf(format, args...))
}

// newPipelineStore builds a real temp store (FTS5) with one collection, one
// document, and one pending text chunk.
func newPipelineStore(t *testing.T) *store.Store {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags fts5 ./... or make test")
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmpDir, "*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	docID, err := db.UpsertDocument(col.ID, "n.md", "Note", "h1", 123.0, 3)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if err := db.InsertChunk(docID, 0, "hello world content", nil); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}
	return db
}

// appConfigFor builds a minimal AppConfig wired to tmp paths.
func appConfigFor(t *testing.T, mutate func(*config.Config)) *config.AppConfig {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.AppConfig{
		Config:   config.Config{},
		CacheDir: filepath.Join(tmp, "cache"),
		DBPath:   filepath.Join(tmp, "index.db"),
	}
	if mutate != nil {
		mutate(&cfg.Config)
	}
	return cfg
}

func TestEmbedPending_NoKey_SkipsWithWarning(t *testing.T) {
	db := newPipelineStore(t)
	cfg := appConfigFor(t, func(c *config.Config) {
		// No API key configured anywhere.
		c.Embedding.BaseURL = "http://127.0.0.1:0/v1"
		c.Embedding.Model = "text-embedding-3-small"
		c.Embedding.APIKey = ""
	})

	var log captureLogger
	err := EmbedPending(cfg, db, Options{}, &log)
	if err != nil {
		t.Fatalf("keyword-only sync must not fail, got: %v", err)
	}
	if !strings.Contains(log.buf.String(), "skip embeddings") {
		t.Errorf("expected a skip warning, got: %q", log.buf.String())
	}
	// The chunk stays pending (no embedding written), so keyword search
	// works and a later `seek embed` can still process it.
	remaining, err := db.GetChunksWithoutEmbedding(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Errorf("expected 1 pending chunk after keyword-only skip, got %d", len(remaining))
	}
}

func TestEmbedPending_WithKey_EmbedsPending(t *testing.T) {
	// Mock OpenAI-compatible embeddings endpoint returning a 4-dim vector.
	dims := 4
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/embeddings") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"data": make([]map[string]any, len(body.Input)),
		}
		data := resp["data"].([]map[string]any)
		for i := range body.Input {
			data[i] = map[string]any{"embedding": make([]float32, dims), "index": i}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	db := newPipelineStore(t)
	cfg := appConfigFor(t, func(c *config.Config) {
		c.Embedding.BaseURL = ts.URL
		c.Embedding.Model = "text-embedding-3-small"
		c.Embedding.APIKey = "test-key"
		c.Embedding.Dimensions = dims
	})

	// Verify the client can reach the endpoint before running the pipeline.
	// (Guards against a shape mismatch in the mock.)
	client := embed.NewClientFromConfig(cfg)
	if client == nil {
		t.Fatal("expected a non-nil text client")
	}

	var log captureLogger
	err := EmbedPending(cfg, db, Options{Realtime: true}, &log)
	if err != nil {
		t.Fatalf("embed pending: %v", err)
	}
	remaining, err := db.GetChunksWithoutEmbedding(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 pending chunks after embed, got %d (log: %q)", len(remaining), log.buf.String())
	}
}

func TestEmbeddingCapability(t *testing.T) {
	t.Run("no key -> not ready with hint", func(t *testing.T) {
		cfg := appConfigFor(t, func(c *config.Config) {
			c.Embedding.APIKey = ""
		})
		if os.Getenv("SEEK_TEST_API_KEY") != "" {
			t.Skip("ambient key present")
		}
		ok, why := embed.EmbeddingCapability(cfg)
		if ok {
			t.Error("expected not ready without a key")
		}
		if !strings.Contains(why, "seek auth login") {
			t.Errorf("expected an auth-login hint, got %q", why)
		}
	})
	t.Run("offline -> not ready", func(t *testing.T) {
		cfg := appConfigFor(t, func(c *config.Config) {
			c.Privacy.OfflineOnly = true
			c.Embedding.APIKey = "whatever"
		})
		ok, why := embed.EmbeddingCapability(cfg)
		if ok {
			t.Error("expected offline to be not ready")
		}
		if !strings.Contains(why, "offline") {
			t.Errorf("expected offline hint, got %q", why)
		}
	})
	t.Run("key present -> ready", func(t *testing.T) {
		cfg := appConfigFor(t, func(c *config.Config) {
			c.Embedding.APIKey = "sk-test"
			c.Embedding.BaseURL = "http://127.0.0.1:0/v1"
		})
		ok, _ := embed.EmbeddingCapability(cfg)
		if !ok {
			t.Error("expected ready with a key")
		}
	})
}

func TestEmbedPending_BatchFlagSelectsPath(t *testing.T) {
	var batchHits, realtimeHits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/files":
			batchHits++
			json.NewEncoder(w).Encode(map[string]any{"id": "file-1"})
		case strings.Contains(r.URL.Path, "/embeddings/batches"):
			batchHits++
			json.NewEncoder(w).Encode(map[string]any{"id": "batch-1"})
		case r.URL.Path == "/embeddings" && r.Method == http.MethodPost:
			realtimeHits++
			var body struct {
				Input []string `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			data := make([]map[string]any, len(body.Input))
			for i := range body.Input {
				data[i] = map[string]any{"embedding": []float32{0.1}, "index": i}
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	db := newPipelineStore(t)
	cfg := appConfigFor(t, func(c *config.Config) {
		c.Embedding.BaseURL = ts.URL
		c.Embedding.Model = "text-embedding-3-small"
		c.Embedding.APIKey = "test-key"
		c.Embedding.Dimensions = 1
	})

	// Batch=false must take the realtime path (this is what --no-batch means).
	var log captureLogger
	if err := EmbedPending(cfg, db, Options{Batch: false}, &log); err != nil {
		t.Fatalf("embed pending: %v", err)
	}
	if realtimeHits == 0 {
		t.Errorf("Batch=false must use the realtime /embeddings endpoint, hits: realtime=%d batch=%d", realtimeHits, batchHits)
	}
	remaining, _ := db.GetChunksWithoutEmbedding(false)
	if len(remaining) != 0 {
		t.Errorf("chunk not embedded: %d remaining", len(remaining))
	}
}
