package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddingConfigIsMultimodal(t *testing.T) {
	cases := []struct {
		name     string
		cfg      EmbeddingConfig
		expectML bool
	}{
		{"explicit flag forces true", EmbeddingConfig{Model: "text-embedding-v4", Multimodal: true}, true},
		{"vl-embedding name", EmbeddingConfig{Model: "qwen3-vl-embedding"}, true},
		{"multimodal name", EmbeddingConfig{Model: "multimodal-embedding-v1"}, true},
		{"text model defaults false", EmbeddingConfig{Model: "text-embedding-3-small"}, false},
		{"empty model defaults false", EmbeddingConfig{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.IsMultimodal(); got != tc.expectML {
				t.Errorf("IsMultimodal() = %v, want %v", got, tc.expectML)
			}
		})
	}
}

func TestDefaultAppConfig(t *testing.T) {
	cacheD := "/tmp/test-cache"
	ac := defaultAppConfig(cacheD)

	if ac.CacheDir != cacheD {
		t.Errorf("CacheDir = %q, want %q", ac.CacheDir, cacheD)
	}
	if ac.DBPath != filepath.Join(cacheD, "index.db") {
		t.Errorf("DBPath = %q, want %q", ac.DBPath, filepath.Join(cacheD, "index.db"))
	}
	if ac.Config.Embedding.BaseURL != DefaultEmbeddingBaseURL {
		t.Errorf("Embedding.BaseURL = %q, want %q", ac.Config.Embedding.BaseURL, DefaultEmbeddingBaseURL)
	}
	if ac.Config.Search.QueryMode != DefaultQueryMode {
		t.Errorf("Search.QueryMode = %q, want %q", ac.Config.Search.QueryMode, DefaultQueryMode)
	}
	if ac.Config.VectorIndex.HNSW.M != DefaultHNSWM {
		t.Errorf("HNSW.M = %d, want %d", ac.Config.VectorIndex.HNSW.M, DefaultHNSWM)
	}
}

func TestApplyFallbacks(t *testing.T) {
	t.Setenv("TEST_API_KEY", "secret-key-123")
	t.Setenv("TEST_XBERG_HOST", "http://xberg.local:8080")

	cfg := Config{
		Embedding: EmbeddingConfig{
			BaseURL: "https://api.example.com/v1",
			APIKey:  "${TEST_API_KEY}",
			Model:   "custom-embed",
		},
		Extractor: ExtractorConfig{
			XbergBaseURL: "${TEST_XBERG_HOST}",
		},
	}

	applyFallbacks(&cfg)

	if cfg.Embedding.APIKey != "secret-key-123" {
		t.Errorf("Embedding.APIKey = %q, want secret-key-123", cfg.Embedding.APIKey)
	}
	if cfg.OCR.BaseURL != "https://api.example.com/v1" {
		t.Errorf("OCR.BaseURL = %q, want https://api.example.com/v1", cfg.OCR.BaseURL)
	}
	if cfg.OCR.APIKey != "secret-key-123" {
		t.Errorf("OCR.APIKey = %q, want secret-key-123", cfg.OCR.APIKey)
	}
	if cfg.OCR.Model != DefaultOCRModel {
		t.Errorf("OCR.Model = %q, want %q", cfg.OCR.Model, DefaultOCRModel)
	}

	if cfg.Rerank.BaseURL != "https://api.example.com/v1" {
		t.Errorf("Rerank.BaseURL = %q, want https://api.example.com/v1", cfg.Rerank.BaseURL)
	}
	if cfg.Rerank.APIKey != "secret-key-123" {
		t.Errorf("Rerank.APIKey = %q, want secret-key-123", cfg.Rerank.APIKey)
	}
	if cfg.Rerank.Model != DefaultRerankModel {
		t.Errorf("Rerank.Model = %q, want %q", cfg.Rerank.Model, DefaultRerankModel)
	}
	if cfg.Rerank.TopN != DefaultRerankTopN {
		t.Errorf("Rerank.TopN = %d, want %d", cfg.Rerank.TopN, DefaultRerankTopN)
	}

	if cfg.Extractor.Backend != DefaultExtractorBackend {
		t.Errorf("Extractor.Backend = %q, want %q", cfg.Extractor.Backend, DefaultExtractorBackend)
	}
	if cfg.Extractor.OutputFormat != DefaultExtractorOutputFormat {
		t.Errorf("Extractor.OutputFormat = %q, want %q", cfg.Extractor.OutputFormat, DefaultExtractorOutputFormat)
	}
	if cfg.Extractor.XbergBaseURL != "http://xberg.local:8080" {
		t.Errorf("Extractor.XbergBaseURL = %q, want http://xberg.local:8080", cfg.Extractor.XbergBaseURL)
	}
	if cfg.Extractor.Timeout != DefaultXbergTimeout {
		t.Errorf("Extractor.Timeout = %v, want %v", cfg.Extractor.Timeout, DefaultXbergTimeout)
	}
}

func TestLoad(t *testing.T) {
	// Create a temporary HOME directory for testing Load()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	ac, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if ac == nil {
		t.Fatalf("Load() returned nil AppConfig")
	}

	if ac.Config.Embedding.BaseURL != DefaultEmbeddingBaseURL {
		t.Errorf("Embedding.BaseURL = %q, want %q", ac.Config.Embedding.BaseURL, DefaultEmbeddingBaseURL)
	}

	// Verify directories were created
	cfgDir := filepath.Join(tmpHome, ".config", "seek")
	if _, err := os.Stat(cfgDir); os.IsNotExist(err) {
		t.Errorf("configDir was not created")
	}
}
