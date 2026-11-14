package embed

import (
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
)

type typedNilQueryEmbedder struct{}

func (*typedNilQueryEmbedder) EmbedQuery(string) ([]float32, error) {
	return nil, nil
}

func TestClientFromConfigTable(t *testing.T) {
	tests := []struct {
		name       string
		offline    bool
		apiKey     string
		model      string
		wantClient bool
		wantVL     bool
	}{
		{
			name:       "key+text model -> text client only",
			apiKey:     "sk",
			model:      "text-embedding-3-small",
			wantClient: true,
			wantVL:     false,
		},
		{
			name:       "no key -> nil clients",
			apiKey:     "",
			model:      "text-embedding-3-small",
			wantClient: false,
			wantVL:     false,
		},
		{
			name:       "offline -> offline client, no VL",
			offline:    true,
			apiKey:     "sk",
			model:      "text-embedding-3-small",
			wantClient: true, // offline client is returned, not nil
			wantVL:     false,
		},
		{
			name:       "multimodal model -> both clients",
			apiKey:     "sk",
			model:      "qwen3-vl-embedding",
			wantClient: true,
			wantVL:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.AppConfig{Config: config.Config{}}
			cfg.Config.Embedding.Model = tt.model
			cfg.Config.Embedding.APIKey = tt.apiKey
			cfg.Config.Embedding.BaseURL = "http://127.0.0.1:0/v1"
			cfg.Config.Embedding.Dimensions = 4
			cfg.Config.Privacy.OfflineOnly = tt.offline

			client := NewClientFromConfig(cfg)
			if (client != nil) != tt.wantClient {
				t.Errorf("text client presence = %v, want %v", client != nil, tt.wantClient)
			}
			vl := NewVLClientFromConfig(cfg)
			if (vl != nil) != tt.wantVL {
				t.Errorf("vl client presence = %v, want %v", vl != nil, tt.wantVL)
			}
		})
	}
}

func TestProviderRegistryUsesCapabilityFactory(t *testing.T) {
	registry := NewProviderRegistry()
	marker := &Client{}
	registry.Register("test", func(*config.AppConfig) (Provider, error) {
		return Provider{Query: marker, Document: marker, Batch: marker}, nil
	})

	provider, err := registry.Build("test", &config.AppConfig{})
	if err != nil {
		t.Fatalf("Build(test): %v", err)
	}
	if provider.Query != marker || provider.Document != marker || provider.Batch != marker {
		t.Fatalf("provider capabilities were not returned by the registered factory")
	}
	if _, err := registry.Build("missing", &config.AppConfig{}); err == nil {
		t.Fatal("Build(missing) succeeded, want registration error")
	}
}

func TestProviderRegistryNormalizesTypedNilCapabilities(t *testing.T) {
	registry := NewProviderRegistry()
	registry.Register("typed-nil", func(*config.AppConfig) (Provider, error) {
		var query *typedNilQueryEmbedder
		return Provider{Query: query}, nil
	})

	provider, err := registry.Build("typed-nil", &config.AppConfig{})
	if err != nil {
		t.Fatalf("Build(typed-nil): %v", err)
	}
	if provider.Query != nil {
		t.Fatal("typed-nil query capability was not normalized")
	}
}

func TestNewProviderFromConfigBuildsConfiguredBundle(t *testing.T) {
	cfg := &config.AppConfig{Config: config.Config{}}
	cfg.Config.Privacy.OfflineOnly = true
	cfg.Config.Embedding.Model = "text-embedding-3-small"

	provider, err := NewProviderFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewProviderFromConfig: %v", err)
	}
	if provider.Query == nil || provider.Document == nil || provider.Batch == nil {
		t.Fatalf("configured provider is missing text capabilities: %#v", provider)
	}
	if provider.VLQuery != nil || provider.VLText != nil || provider.VLImage != nil {
		t.Fatalf("offline configured provider unexpectedly exposes VL capabilities")
	}
}

func TestNewVLClientFromConfigOfflineUsesLocalEmbeddingFallback(t *testing.T) {
	cfg := &config.AppConfig{Config: config.Config{}}
	cfg.Config.Privacy.OfflineOnly = true
	cfg.Config.Embedding.BaseURL = "http://127.0.0.1:11434/v1"
	cfg.Config.Embedding.APIKey = "local"
	cfg.Config.Embedding.Model = "qwen3-vl-embedding"

	client := NewVLClientFromConfig(cfg)
	if client == nil {
		t.Fatal("expected local VL client")
	}
	if client.endpoint != cfg.Config.Embedding.BaseURL {
		t.Fatalf("endpoint = %q, want %q", client.endpoint, cfg.Config.Embedding.BaseURL)
	}
}

func TestNewProviderFromConfigWithoutKeyHasNilTextCapabilities(t *testing.T) {
	cfg := &config.AppConfig{Config: config.Config{}}
	cfg.Config.Embedding.Model = "text-embedding-3-small"

	provider, err := NewProviderFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewProviderFromConfig: %v", err)
	}
	if provider.Query != nil || provider.Document != nil || provider.Batch != nil {
		t.Fatalf("missing-key provider exposes text capabilities: %#v", provider)
	}
}

func TestProviderCapabilitiesTable(t *testing.T) {
	tests := []struct {
		name           string
		baseURL        string
		model          string
		wantKind       ProviderKind
		wantRealtime   bool
		wantAsyncBatch bool
	}{
		{
			name:           "numeric loopback -> local, no async batch",
			baseURL:        "http://127.0.0.1:8000/v1",
			model:          "custom-embed",
			wantKind:       ProviderKindLocal,
			wantRealtime:   true,
			wantAsyncBatch: false,
		},
		{
			name:           "ollama loopback port -> ollama, no async batch",
			baseURL:        "http://127.0.0.1:11434/v1",
			model:          "nomic-embed-text",
			wantKind:       ProviderKindOllama,
			wantRealtime:   true,
			wantAsyncBatch: false,
		},
		{
			name:           "ollama localhost hostname -> ollama, no async batch",
			baseURL:        "http://localhost:11434/v1",
			model:          "nomic-embed-text",
			wantKind:       ProviderKindOllama,
			wantRealtime:   true,
			wantAsyncBatch: false,
		},
		{
			name:           "ollama model name on loopback -> ollama",
			baseURL:        "http://127.0.0.1:8000/v1",
			model:          "ollama/nomic-embed-text",
			wantKind:       ProviderKindOllama,
			wantRealtime:   true,
			wantAsyncBatch: false,
		},
		{
			name:           "fastembed local helper -> local, no async batch",
			baseURL:        "http://127.0.0.1:8001/v1",
			model:          "BAAI/bge-small-en-v1.5",
			wantKind:       ProviderKindLocal,
			wantRealtime:   true,
			wantAsyncBatch: false,
		},
		{
			name:           "generic remote -> generic, no async batch",
			baseURL:        "https://embeddings.example.com/v1",
			model:          "custom-embed",
			wantKind:       ProviderKindGeneric,
			wantRealtime:   true,
			wantAsyncBatch: false,
		},
		{
			name:           "dashscope hosted -> hosted, async batch",
			baseURL:        "https://dashscope.aliyuncs.com/compatible-mode/v1",
			model:          "text-embedding-v4",
			wantKind:       ProviderKindHosted,
			wantRealtime:   true,
			wantAsyncBatch: true,
		},
		{
			name:           "openai hosted -> hosted, async batch",
			baseURL:        "https://api.openai.com/v1",
			model:          "text-embedding-3-small",
			wantKind:       ProviderKindHosted,
			wantRealtime:   true,
			wantAsyncBatch: true,
		},
		{
			name:           "offline-only loopback -> local, no async batch",
			baseURL:        "http://127.0.0.1:11434/v1",
			model:          "nomic-embed-text",
			wantKind:       ProviderKindOllama,
			wantRealtime:   true,
			wantAsyncBatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.AppConfig{Config: config.Config{}}
			cfg.Config.Embedding.BaseURL = tt.baseURL
			cfg.Config.Embedding.Model = tt.model
			cfg.Config.Embedding.APIKey = "sk"

			if got := DetectProviderKind(cfg); got != tt.wantKind {
				t.Errorf("DetectProviderKind = %q, want %q", got, tt.wantKind)
			}
			caps := ProviderCapabilities(cfg)
			if caps.RealtimeEmbeddings != tt.wantRealtime {
				t.Errorf("RealtimeEmbeddings = %v, want %v", caps.RealtimeEmbeddings, tt.wantRealtime)
			}
			if caps.AsyncBatch != tt.wantAsyncBatch {
				t.Errorf("AsyncBatch = %v, want %v", caps.AsyncBatch, tt.wantAsyncBatch)
			}
		})
	}
}

func TestConfiguredProviderExposesCapabilities(t *testing.T) {
	cfg := &config.AppConfig{Config: config.Config{}}
	cfg.Config.Embedding.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	cfg.Config.Embedding.Model = "text-embedding-v4"
	cfg.Config.Embedding.APIKey = "sk"

	provider, err := NewProviderFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewProviderFromConfig: %v", err)
	}
	if !provider.Capabilities.RealtimeEmbeddings {
		t.Error("configured provider should support realtime embeddings")
	}
	if !provider.Capabilities.AsyncBatch {
		t.Error("dashscope configured provider should declare async batch")
	}
}
