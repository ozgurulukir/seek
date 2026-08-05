package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

type Config struct {
	Embedding EmbeddingConfig `yaml:"embedding"`
	OCR       OCRConfig       `yaml:"ocr,omitempty"`
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
	os.MkdirAll(cfgDir, 0755)
	os.MkdirAll(cacheD, 0755)

	ac := &AppConfig{
		CacheDir: cacheD,
		DBPath:   filepath.Join(cacheD, "index.db"),
	}

	// Defaults
	ac.Config.Embedding = EmbeddingConfig{
		BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:      "text-embedding-v4",
		Dimensions: 1024,
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

	// OCR falls back to the embedding provider when not explicitly set.
	if ac.Config.OCR.BaseURL == "" {
		ac.Config.OCR.BaseURL = ac.Config.Embedding.BaseURL
	}
	if ac.Config.OCR.APIKey == "" {
		ac.Config.OCR.APIKey = ac.Config.Embedding.APIKey
	}
	if ac.Config.OCR.Model == "" {
		ac.Config.OCR.Model = "qwen-vl-ocr"
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
	os.MkdirAll(dir, 0755)

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0600)
}

// expandEnv resolves ${VAR} references in a string.
func expandEnv(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		name := s[2 : len(s)-1]
		return os.Getenv(name)
	}
	return s
}
