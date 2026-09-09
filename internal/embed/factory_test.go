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
