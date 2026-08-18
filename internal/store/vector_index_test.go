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
							HNSW:    config.HNSWConfig{PersistPath: ""}, // No persist to keep memory-only
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
				// Assert the correct underlying type
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

	// Write garbage to the index file
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

	// Verify we got a fresh hnsw index (0 elements)
	if idx.Len() != 0 {
		t.Errorf("expected empty fresh index, got length %d", idx.Len())
	}
}
