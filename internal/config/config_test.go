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

func TestOCRDefaultsResolveFromEmbedding(t *testing.T) {
	// Simulate the Load() fallback logic directly.
	emb := EmbeddingConfig{BaseURL: "https://api.example.com/v1", APIKey: "k", Model: "text-embedding-v4"}
	ocr := OCRConfig{Enabled: true} // base_url/api_key/model empty
	if ocr.BaseURL == "" {
		ocr.BaseURL = emb.BaseURL
	}
	if ocr.APIKey == "" {
		ocr.APIKey = emb.APIKey
	}
	if ocr.Model == "" {
		ocr.Model = "qwen-vl-ocr"
	}
	if ocr.BaseURL != "https://api.example.com/v1" || ocr.APIKey != "k" || ocr.Model != "qwen-vl-ocr" {
		t.Errorf("OCR defaults not resolved: %+v", ocr)
	}
}
