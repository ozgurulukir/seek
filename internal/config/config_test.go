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
	t.Setenv("USERPROFILE", tmpHome)

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

func TestLoad_EnvVarPropagation(t *testing.T) {
	// Isolate the real ~/.config/seek by pointing HOME at a temp dir.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	const apiKey = "expanded-key-42"
	t.Setenv("SEEK_TEST_API_KEY", apiKey)

	// Embedding api_key references the env var; ocr/rerank keys are absent,
	// so they must fall back to the *expanded* embedding key.
	yamlCfg := []byte("embedding:\n" +
		"  api_key: ${SEEK_TEST_API_KEY}\n" +
		"ocr: {}\n" +
		"rerank: {}\n")
	cfgDir := filepath.Join(tmpHome, ".config", "seek")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("creating temp config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), yamlCfg, 0o600); err != nil {
		t.Fatalf("writing temp config.yaml: %v", err)
	}

	ac, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	// 1. Env expansion of the embedding key itself.
	if got := ac.Config.Embedding.APIKey; got != apiKey {
		t.Errorf("Embedding.APIKey = %q, want %q (env expansion)", got, apiKey)
	}
	// 2. The expanded key propagates to the OCR and Rerank APIKey fallbacks.
	if got := ac.Config.OCR.APIKey; got != apiKey {
		t.Errorf("OCR.APIKey = %q, want %q (fallback to expanded embedding key)", got, apiKey)
	}
	if got := ac.Config.Rerank.APIKey; got != apiKey {
		t.Errorf("Rerank.APIKey = %q, want %q (fallback to expanded embedding key)", got, apiKey)
	}
	// 3. Other rerank fallbacks still apply.
	if got := ac.Config.Rerank.Model; got != DefaultRerankModel {
		t.Errorf("Rerank.Model = %q, want %q", got, DefaultRerankModel)
	}
	if got := ac.Config.Rerank.TopN; got != DefaultRerankTopN {
		t.Errorf("Rerank.TopN = %d, want %d", got, DefaultRerankTopN)
	}
}

func TestTaskPrefixes(t *testing.T) {
	cases := []struct {
		name         string
		cfg          EmbeddingConfig
		wantQuery    string
		wantDocument string
	}{
		{"nomic full", EmbeddingConfig{Model: "nomic-embed-text"}, "search_query: ", "search_document: "},
		{"nomic versioned", EmbeddingConfig{Model: "nomic-embed-text-v1.5"}, "search_query: ", "search_document: "},
		{"e5 bare", EmbeddingConfig{Model: "e5-small-v2"}, "query: ", "passage: "},
		{"e5 namespaced", EmbeddingConfig{Model: "intfloat/multilingual-e5-large"}, "query: ", "passage: "},
		{"bge v1 en query only", EmbeddingConfig{Model: "bge-large-en-v1.5"}, "Represent this sentence for searching relevant passages: ", ""},
		{"bge m3 none", EmbeddingConfig{Model: "bge-m3"}, "", ""},
		{"bge reranker none", EmbeddingConfig{Model: "bge-reranker-v2-m3"}, "", ""},
		{"openai none", EmbeddingConfig{Model: "text-embedding-3-small"}, "", ""},
		{"qwen none", EmbeddingConfig{Model: "text-embedding-v4"}, "", ""},
		{"explicit override wins", EmbeddingConfig{
			Model:      "nomic-embed-text",
			TaskPrefix: TaskPrefixConfig{Query: "q: ", Document: "d: "},
		}, "q: ", "d: "},
		{"explicit query only, rest auto", EmbeddingConfig{
			Model:      "nomic-embed-text",
			TaskPrefix: TaskPrefixConfig{Query: "sorgu: "},
		}, "sorgu: ", "search_document: "},
		{"empty model none", EmbeddingConfig{}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQuery, gotDocument := tc.cfg.TaskPrefixes()
			if gotQuery != tc.wantQuery {
				t.Errorf("query prefix = %q, want %q", gotQuery, tc.wantQuery)
			}
			if gotDocument != tc.wantDocument {
				t.Errorf("document prefix = %q, want %q", gotDocument, tc.wantDocument)
			}
		})
	}
}

func TestTaskPrefixesDisableAutoDetect(t *testing.T) {
	// A model-family name that would auto-detect is kept raw when
	// disable_auto_detect is set, and explicit prefixes still apply.
	cfg := EmbeddingConfig{
		Model: "nomic-embed-text",
		TaskPrefix: TaskPrefixConfig{
			Query:             "q: ",
			DisableAutoDetect: true,
		},
	}
	gotQuery, gotDocument := cfg.TaskPrefixes()
	if gotQuery != "q: " {
		t.Errorf("query prefix = %q, want %q", gotQuery, "q: ")
	}
	if gotDocument != "" {
		t.Errorf("document prefix = %q, want empty (no auto-detect)", gotDocument)
	}

	// bge-en-icl looks like a BGE v1 English model but is instruction-free;
	// disable_auto_detect is the escape hatch for it.
	lookalike := EmbeddingConfig{
		Model:      "bge-en-icl",
		TaskPrefix: TaskPrefixConfig{DisableAutoDetect: true},
	}
	if q, d := lookalike.TaskPrefixes(); q != "" || d != "" {
		t.Errorf("lookalike prefixes = (%q, %q), want empty", q, d)
	}
}
