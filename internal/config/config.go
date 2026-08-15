package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type EmbeddingConfig struct {
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions,omitempty"`
	// VLBaseURL is the multimodal (vision-language) embedding endpoint.
	// Leave empty to use the DashScope default when the model is multimodal.
	VLBaseURL string `yaml:"vl_base_url,omitempty"`
	// Multimodal forces the vision-language (image+text) embedding path.
	// When false, it is inferred from the model name (vl-embedding / multimodal).
	Multimodal bool `yaml:"multimodal,omitempty"`
}

// IsMultimodal reports whether to use the vision-language embedding client.
func (e EmbeddingConfig) IsMultimodal() bool {
	if e.Multimodal {
		return true
	}
	return strings.Contains(e.Model, "vl-embedding") || strings.Contains(e.Model, "multimodal")
}

// OCRConfig configures text extraction from rasterized PDF pages (scanned docs).
// It uses the OpenAI-compatible chat-completions vision format, so any provider
// that exposes a vision/OCR model works (DashScope qwen-vl-ocr, OpenAI gpt-4o, etc.).
// Empty base_url/model fall back to the embedding provider's settings.
type OCRConfig struct {
	Enabled bool   `yaml:"enabled,omitempty"`
	BaseURL string `yaml:"base_url,omitempty"`
	APIKey  string `yaml:"api_key,omitempty"`
	Model   string `yaml:"model,omitempty"`
}

// RerankConfig configures optional cross-encoder reranking.
// If enabled is true in config, search queries automatically rerank top candidate
// hits before returning results.
type RerankConfig struct {
	Enabled bool   `yaml:"enabled,omitempty"`
	BaseURL string `yaml:"base_url,omitempty"`
	APIKey  string `yaml:"api_key,omitempty"`
	Model   string `yaml:"model,omitempty"`
	TopN    int    `yaml:"top_n,omitempty"`
}

// SearchConfig configures the search engine behavior.
type SearchConfig struct {
	// QueryMode is the default query parsing mode: "raw" (FTS5 MATCH passthrough)
	// or "parsed" (structured query parser). Invalid parsed queries fall back to raw.
	QueryMode string `yaml:"query_mode,omitempty"`
	// DefaultLimit is the default max results when not specified via CLI.
	DefaultLimit int `yaml:"default_limit,omitempty"`
	// RRFK is the RRF (Reciprocal Rank Fusion) constant.
	RRFK int `yaml:"rrf_k,omitempty"`
}

// FilterConfig configures filter behavior.
type FilterConfig struct {
	Enabled           bool   `yaml:"enabled,omitempty"`
	DefaultCollection string `yaml:"default_collection,omitempty"`
}

// AggregationConfig configures aggregation behavior.
type AggregationConfig struct {
	Enabled bool `yaml:"enabled,omitempty"`
}

// VectorIndexConfig configures the vector index backend.
type VectorIndexConfig struct {
	// Backend is the vector index backend: "hnsw" or "linear".
	Backend string `yaml:"backend,omitempty"`
	// HNSW holds HNSW-specific parameters.
	HNSW HNSWConfig `yaml:"hnsw,omitempty"`
}

// HNSWConfig holds HNSW index parameters.
type HNSWConfig struct {
	M              int    `yaml:"m,omitempty"`
	EFConstruction int    `yaml:"ef_construction,omitempty"` // reserved; coder/hnsw v0.6.1 has no public field for this
	EFSearch       int    `yaml:"ef_search,omitempty"`
	PersistPath    string `yaml:"persist_path,omitempty"`
	Dimension      int    `yaml:"dimension,omitempty"`
}

// CompressionConfig configures chunk content compression.
type CompressionConfig struct {
	// Algorithm is the compression algorithm: "zstd", "lz4", or "none".
	Algorithm string `yaml:"algorithm,omitempty"`
	// Level is the compression level (1-22 for zstd, 1-12 for lz4).
	Level int `yaml:"level,omitempty"`
}

// ExtractorConfig configures the extraction domain — how files on disk are
// turned into indexable text. The backend is selected globally: "builtin"
// (native Go extractors, the default) or "xberg" (a remote xberg serve HTTP
// service supporting 100+ document formats). It may be overridden per-command
// via the --backend flag.
type ExtractorConfig struct {
	// Backend is the extractor backend: "builtin" or "xberg".
	Backend string `yaml:"backend,omitempty"`
	// OutputFormat is the text format requested from xberg ("plain", "markdown",
	// "djot", "html"). Ignored by the builtin backend.
	OutputFormat string `yaml:"output_format,omitempty"`
	// XbergBaseURL is the xberg serve endpoint (e.g. http://127.0.0.1:8000).
	XbergBaseURL string `yaml:"xberg_base_url,omitempty"`
	// Timeout is the per-request timeout for xberg extraction. Defaults to
	// DefaultXbergTimeout when zero.
	Timeout time.Duration `yaml:"timeout,omitempty"`
}

// Config holds all configuration sections.
type Config struct {
	Embedding    EmbeddingConfig   `yaml:"embedding"`
	OCR          OCRConfig         `yaml:"ocr,omitempty"`
	Rerank       RerankConfig      `yaml:"rerank,omitempty"`
	Search       SearchConfig      `yaml:"search,omitempty"`
	Filters      FilterConfig      `yaml:"filters,omitempty"`
	Aggregations AggregationConfig `yaml:"aggregations,omitempty"`
	VectorIndex  VectorIndexConfig `yaml:"vector_index,omitempty"`
	Compression  CompressionConfig `yaml:"compression,omitempty"`
	Extractor    ExtractorConfig   `yaml:"extractor,omitempty"`
}

type AppConfig struct {
	Config   Config
	CacheDir string
	DBPath   string
}

func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "seek")
}

func cacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "seek")
}

func Load() (*AppConfig, error) {
	cfgDir := configDir()
	cacheD := cacheDir()
	os.MkdirAll(cfgDir, DefaultDirPerms)
	os.MkdirAll(cacheD, DefaultDirPerms)

	ac := &AppConfig{
		CacheDir: cacheD,
		DBPath:   filepath.Join(cacheD, "index.db"),
	}

	// Defaults
	ac.Config.Embedding = EmbeddingConfig{
		BaseURL:    DefaultEmbeddingBaseURL,
		Model:      DefaultEmbeddingModel,
		Dimensions: DefaultEmbeddingDimensions,
	}
	ac.Config.Search = SearchConfig{
		QueryMode:    DefaultQueryMode,
		DefaultLimit: DefaultSearchLimit,
		RRFK:         DefaultRRFK,
	}
	ac.Config.Filters = FilterConfig{
		Enabled: true,
	}
	ac.Config.Aggregations = AggregationConfig{
		Enabled: true,
	}
	ac.Config.VectorIndex = VectorIndexConfig{
		Backend: DefaultVectorIndexBackend,
		HNSW: HNSWConfig{
			M:              DefaultHNSWM,
			EFConstruction: DefaultHNSEFConstruction,
			EFSearch:       DefaultHNSEFSearch,
			PersistPath:    filepath.Join(cacheD, "hnsw.index"),
			Dimension:      DefaultHNSWDimension,
		},
	}
	ac.Config.Compression = CompressionConfig{
		Algorithm: DefaultCompressionAlgorithm,
		Level:     DefaultCompressionLevel,
	}
	ac.Config.Extractor = ExtractorConfig{
		Backend:      DefaultExtractorBackend,
		OutputFormat: DefaultExtractorOutputFormat,
		XbergBaseURL: DefaultXbergBaseURL,
		Timeout:      DefaultXbergTimeout,
	}

	cfgPath := filepath.Join(cfgDir, "config.yaml")
	if data, err := os.ReadFile(cfgPath); err == nil {
		if err := yaml.Unmarshal(data, &ac.Config); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	// Resolve env vars in api_key (e.g. ${DASHSCOPE_API_KEY})
	ac.Config.Embedding.APIKey = expandEnv(ac.Config.Embedding.APIKey)
	ac.Config.OCR.APIKey = expandEnv(ac.Config.OCR.APIKey)
	ac.Config.Rerank.APIKey = expandEnv(ac.Config.Rerank.APIKey)

	// OCR falls back to the embedding provider when not explicitly set.
	if ac.Config.OCR.BaseURL == "" {
		ac.Config.OCR.BaseURL = ac.Config.Embedding.BaseURL
	}
	if ac.Config.OCR.APIKey == "" {
		ac.Config.OCR.APIKey = ac.Config.Embedding.APIKey
	}
	if ac.Config.OCR.Model == "" {
		ac.Config.OCR.Model = DefaultOCRModel
	}

	// Rerank falls back to the embedding provider's baseURL/APIKey if empty
	if ac.Config.Rerank.BaseURL == "" {
		ac.Config.Rerank.BaseURL = ac.Config.Embedding.BaseURL
	}
	if ac.Config.Rerank.APIKey == "" {
		ac.Config.Rerank.APIKey = ac.Config.Embedding.APIKey
	}
	if ac.Config.Rerank.Model == "" {
		ac.Config.Rerank.Model = DefaultRerankModel
	}
	if ac.Config.Rerank.TopN == 0 {
		ac.Config.Rerank.TopN = DefaultRerankTopN
	}

	// Extractor defaults: fill any unset field. BaseURL may embed env vars
	// (e.g. ${XBERG_HOST}), so expand it like the API keys above.
	ac.Config.Extractor.XbergBaseURL = expandEnv(ac.Config.Extractor.XbergBaseURL)
	if ac.Config.Extractor.Backend == "" {
		ac.Config.Extractor.Backend = DefaultExtractorBackend
	}
	if ac.Config.Extractor.OutputFormat == "" {
		ac.Config.Extractor.OutputFormat = DefaultExtractorOutputFormat
	}
	if ac.Config.Extractor.XbergBaseURL == "" {
		ac.Config.Extractor.XbergBaseURL = DefaultXbergBaseURL
	}
	if ac.Config.Extractor.Timeout == 0 {
		ac.Config.Extractor.Timeout = DefaultXbergTimeout
	}

	return ac, nil
}

func (ac *AppConfig) RequireEmbeddingKey() (string, error) {
	key := ac.Config.Embedding.APIKey
	if key == "" {
		return "", fmt.Errorf("embedding API key not configured\nRun 'seek auth login' or add api_key to ~/.config/seek/config.yaml")
	}
	return key, nil
}

func (ac *AppConfig) ConfigPath() string {
	return filepath.Join(configDir(), "config.yaml")
}

func Save(cfg Config) error {
	dir := configDir()
	os.MkdirAll(dir, DefaultDirPerms)

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0600)
}

// expandEnv resolves $VAR and ${VAR} references anywhere in a string.
// This is a thin wrapper over os.ExpandEnv so embedded references like
// "Bearer ${TOKEN}" or "https://${HOST}:${PORT}" are substituted, not just
// values that are exactly "${VAR}".
func expandEnv(s string) string {
	return os.ExpandEnv(s)
}
