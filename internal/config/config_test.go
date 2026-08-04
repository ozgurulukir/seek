package config

import "testing"

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
