package store

import (
	"os"
	"path/filepath"
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
