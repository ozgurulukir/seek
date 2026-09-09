package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
)

func TestNewVectorIndex(t *testing.T) {
	cases := []struct {
		name      string
		cfg       func(cacheDir string) *config.AppConfig
		wantType  string
		expectErr bool
	}{
		{
			name: "default_fallback_to_hnsw",
			cfg: func(cacheDir string) *config.AppConfig {
				return &config.AppConfig{
					Config: config.Config{
						Embedding: config.EmbeddingConfig{Dimensions: 0},
						VectorIndex: config.VectorIndexConfig{
							Backend: "",
							HNSW:    config.HNSWConfig{PersistPath: ""},
						},
					},
					CacheDir: cacheDir,
				}
			},
			wantType: "*store.hnswIndex",
		},
		{
			name: "explicit_hnsw",
			cfg: func(cacheDir string) *config.AppConfig {
				return &config.AppConfig{
					Config: config.Config{
						Embedding: config.EmbeddingConfig{Dimensions: 512},
						VectorIndex: config.VectorIndexConfig{
							Backend: "hnsw",
							HNSW:    config.HNSWConfig{M: 16, EFSearch: 50, PersistPath: filepath.Join(cacheDir, "test.index")},
						},
					},
					CacheDir: cacheDir,
				}
			},
			wantType: "*store.hnswIndex",
		},
		{
			name: "explicit_linear",
			cfg: func(cacheDir string) *config.AppConfig {
				return &config.AppConfig{
					Config: config.Config{
						Embedding: config.EmbeddingConfig{Dimensions: 128},
						VectorIndex: config.VectorIndexConfig{
							Backend: "linear",
						},
					},
					CacheDir: cacheDir,
				}
			},
			wantType: "*store.linearIndex",
		},
		{
			name: "unknown_backend_fallback_to_linear",
			cfg: func(cacheDir string) *config.AppConfig {
				return &config.AppConfig{
					Config: config.Config{
						Embedding: config.EmbeddingConfig{Dimensions: 128},
						VectorIndex: config.VectorIndexConfig{
							Backend: "unknown-thing",
						},
					},
					CacheDir: cacheDir,
				}
			},
			wantType: "*store.linearIndex",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg(t.TempDir())
			idx, err := NewVectorIndex(cfg)

			if (err != nil) != tc.expectErr {
				t.Fatalf("expected error: %v, got: %v", tc.expectErr, err)
			}

			if err == nil {
				gotType := ""
				switch idx.(type) {
				case *hnswIndex:
					gotType = "*store.hnswIndex"
				case *linearIndex:
					gotType = "*store.linearIndex"
				default:
					t.Fatalf("unexpected vector index type returned")
				}

				if gotType != tc.wantType {
					t.Errorf("NewVectorIndex returned %s, want %s", gotType, tc.wantType)
				}
			}
		})
	}
}

// TestNewVectorIndex_HNSWCorruption verifies that a corrupted index file causes NewVectorIndex
// to ignore the corrupt file and return a fresh HNSW index instead of returning an error.
func TestNewVectorIndex_HNSWCorruption(t *testing.T) {
	cacheDir := t.TempDir()
	indexPath := filepath.Join(cacheDir, "corrupt.index")

	if err := os.WriteFile(indexPath, []byte("this is not a valid hnsw index format"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	cfg := &config.AppConfig{
		Config: config.Config{
			Embedding: config.EmbeddingConfig{Dimensions: 128},
			VectorIndex: config.VectorIndexConfig{
				Backend: "hnsw",
				HNSW:    config.HNSWConfig{M: 16, EFSearch: 50, PersistPath: indexPath},
			},
		},
		CacheDir: cacheDir,
	}

	idx, err := NewVectorIndex(cfg)
	if err != nil {
		t.Fatalf("expected NewVectorIndex to recover from corrupt index, got error: %v", err)
	}

	if idx.Len() != 0 {
		t.Errorf("expected empty fresh index, got length %d", idx.Len())
	}
}

func TestStoreCloseFlushesPersistentHNSW(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	indexPath := filepath.Join(t.TempDir(), "vectors.hnsw")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	idx, err := newHNSWIndex(3, 16, 50)
	if err != nil {
		t.Fatalf("newHNSWIndex: %v", err)
	}
	idx.persistPath = indexPath
	if err := idx.Add(42, []float32{0.1, 0.2, 0.3}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.SetVectorIndex(idx)

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("expected flushed HNSW graph: %v", err)
	}
	if _, err := os.Stat(indexPath + ".meta.json"); err != nil {
		t.Fatalf("expected flushed HNSW manifest: %v", err)
	}

	loaded, err := newHNSWIndex(3, 16, 50)
	if err != nil {
		t.Fatalf("reload flushed HNSW: %v", err)
	}
	if err := loaded.Load(indexPath); err != nil {
		t.Fatalf("load flushed HNSW: %v", err)
	}
	if loaded.Len() != 1 || !loaded.Contains(42) {
		t.Fatalf("reloaded HNSW = len %d, contains(42)=%v; want one persisted vector", loaded.Len(), loaded.Contains(42))
	}

	// Close is a public lifecycle boundary and must be safe to call again.
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestStoreRecoverVectorIndexOnGenerationMismatch(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("notes", CollectionTypeMarkdown, t.TempDir(), "*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	docID, err := s.UpsertDocument(col.ID, "note.md", "Note", "hash", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if err := s.InsertChunk(docID, 0, "content", []float32{1, 0, 0}); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}
	var chunkID int64
	if err := s.db.QueryRow(`SELECT id FROM chunks WHERE document_id = ?`, docID).Scan(&chunkID); err != nil {
		t.Fatalf("read chunk id: %v", err)
	}

	idx, err := newHNSWIndex(3, 16, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(chunkID, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	idx.generation = "old-generation"
	idx.persistPath = filepath.Join(t.TempDir(), "vectors.hnsw")
	if err := idx.Save(idx.persistPath); err != nil {
		t.Fatalf("save stale index: %v", err)
	}
	if err := s.UpdateChunkEmbedding(chunkID, []float32{0, 1, 0}); err != nil {
		t.Fatalf("UpdateChunkEmbedding: %v", err)
	}
	s.SetVectorIndex(idx)

	if err := s.RecoverVectorIndex(context.Background()); err != nil {
		t.Fatalf("RecoverVectorIndex: %v", err)
	}
	if !idx.Contains(chunkID) || idx.Warning() == "" {
		t.Fatalf("recovered index contains=%v warning=%q, want rebuilt graph and warning", idx.Contains(chunkID), idx.Warning())
	}
}

func TestStoreCloseReturnsVectorFlushError(t *testing.T) {
	s := newTestStore(t)
	idx, err := newHNSWIndex(3, 16, 50)
	if err != nil {
		t.Fatal(err)
	}
	idx.persistPath = filepath.Join(t.TempDir(), "missing", "vectors.hnsw")
	if err := idx.Add(1, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	s.SetVectorIndex(idx)

	if err := s.Close(); err == nil {
		t.Fatal("Close returned nil after vector flush failure")
	}
	if err := s.Close(); err == nil {
		t.Fatal("second Close returned nil after the original flush failure")
	}
	if err := s.db.Ping(); err == nil {
		t.Fatal("database remained open after Close returned a flush error")
	}
}

func TestHNSWIndex_Add(t *testing.T) {
	idx, err := newHNSWIndex(3, 16, 50)
	if err != nil {
		t.Fatalf("failed to create hnsw index: %v", err)
	}

	err = idx.Add(1, []float32{0.1, 0.2, 0.3})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if idx.Len() != 1 {
		t.Errorf("expected length 1, got %d", idx.Len())
	}

	if !idx.dirty {
		t.Errorf("expected dirty flag to be true")
	}

	err = idx.Add(2, []float32{0.1, 0.2})
	if err == nil {
		t.Errorf("expected error for dimension mismatch, got nil")
	}
}

func TestHNSWIndexSaveLoadRoundTripWithManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vectors.hnsw")
	idx, err := newHNSWIndex(3, 16, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(7, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path + ".meta.json"); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}

	loaded, err := newHNSWIndex(3, 16, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Len() != 1 || !loaded.Contains(7) {
		t.Fatalf("loaded index = len %d, contains(7)=%v", loaded.Len(), loaded.Contains(7))
	}
}

func TestHNSWIndexRejectsManifestDimensionMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vectors.hnsw")
	idx, err := newHNSWIndex(3, 16, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(1, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Save(path); err != nil {
		t.Fatal(err)
	}
	wrong, err := newHNSWIndex(4, 16, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrong.Load(path); err == nil || !strings.Contains(err.Error(), "manifest mismatch") {
		t.Fatalf("load error = %v, want manifest mismatch", err)
	}
}

func TestLinearIndex_Add(t *testing.T) {
	idx := newLinearIndex(3)

	err := idx.Add(1, []float32{0.1, 0.2, 0.3})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if idx.Len() != 1 {
		t.Errorf("expected length 1, got %d", idx.Len())
	}

	err = idx.Add(2, []float32{0.1, 0.2})
	if err == nil {
		t.Errorf("expected error for dimension mismatch, got nil")
	}
}

func TestHNSWIndex_Search(t *testing.T) {
	dim := 3
	idx, err := newHNSWIndex(dim, 16, 50)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	query := []float32{1.0, 0.0, 0.0}
	results, err := idx.Search(query, 2)
	if err != nil {
		t.Fatalf("unexpected error searching empty index: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}

	err = idx.Add(1, []float32{1.0, 0.0, 0.0})
	if err != nil {
		t.Fatalf("failed to add vector 1: %v", err)
	}
	err = idx.Add(2, []float32{0.0, 1.0, 0.0})
	if err != nil {
		t.Fatalf("failed to add vector 2: %v", err)
	}
	err = idx.Add(3, []float32{0.0, 0.0, 1.0})
	if err != nil {
		t.Fatalf("failed to add vector 3: %v", err)
	}

	results, err = idx.Search(query, 2)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ChunkID != 1 {
		t.Errorf("expected best match to be chunk 1, got %d", results[0].ChunkID)
	}
	if results[0].Score <= 0.99 {
		t.Errorf("expected score close to 1.0 for exact match, got %f", results[0].Score)
	}

	_, err = idx.Search([]float32{1.0, 0.0}, 2)
	if err == nil {
		t.Error("expected error for dimension mismatch, got nil")
	}

	results, err = idx.Search([]float32{1.0, 0.0, 0.0}, 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}
