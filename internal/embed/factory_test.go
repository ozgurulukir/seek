package embed

import (
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
)

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
