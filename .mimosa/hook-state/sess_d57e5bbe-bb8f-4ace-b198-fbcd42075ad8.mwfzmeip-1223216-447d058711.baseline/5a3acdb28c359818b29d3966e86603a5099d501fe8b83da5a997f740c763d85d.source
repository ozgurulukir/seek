package config

import (
	"testing"
)

func TestMaskKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "empty string",
			key:      "",
			expected: "",
		},
		{
			name:     "single character",
			key:      "a",
			expected: "********",
		},
		{
			name:     "exactly 8 characters",
			key:      "12345678",
			expected: "********",
		},
		{
			name:     "9 characters",
			key:      "123456789",
			expected: "1234...6789",
		},
		{
			name:     "long API key",
			key:      "sk-proj-1234567890abcdef",
			expected: "sk-p...cdef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskKey(tt.key)
			if got != tt.expected {
				t.Errorf("MaskKey(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestEmbeddingConfig_Redacted(t *testing.T) {
	orig := EmbeddingConfig{
		BaseURL:    "https://api.openai.com/v1",
		APIKey:     "sk-proj-1234567890abcdef",
		Model:      "text-embedding-3-small",
		Dimensions: 1536,
		Host:       "127.0.0.1",
		Port:       8080,
		VLBaseURL:  "https://dashscope.aliyuncs.com/compatible-mode/v1",
	}

	redacted := orig.Redacted()

	// Verify original is untouched
	if orig.APIKey != "sk-proj-1234567890abcdef" {
		t.Errorf("original EmbeddingConfig was mutated: APIKey = %q", orig.APIKey)
	}

	// Verify redacted key is masked
	expectedKey := "sk-p...cdef"
	if redacted.APIKey != expectedKey {
		t.Errorf("redacted.APIKey = %q, want %q", redacted.APIKey, expectedKey)
	}

	// Verify other fields match original
	if redacted.BaseURL != orig.BaseURL ||
		redacted.Model != orig.Model ||
		redacted.Dimensions != orig.Dimensions ||
		redacted.Host != orig.Host ||
		redacted.Port != orig.Port ||
		redacted.VLBaseURL != orig.VLBaseURL {
		t.Errorf("redacted fields were modified unexpectedly: %+v", redacted)
	}
}

func TestOCRConfig_Redacted(t *testing.T) {
	orig := OCRConfig{
		Enabled:   true,
		BaseURL:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
		APIKey:    "sk-proj-1234567890abcdef",
		Model:     "qwen-vl-ocr",
		MaxTokens: 2048,
	}

	redacted := orig.Redacted()

	// Verify original is untouched
	if orig.APIKey != "sk-proj-1234567890abcdef" {
		t.Errorf("original OCRConfig was mutated: APIKey = %q", orig.APIKey)
	}

	// Verify redacted key is masked
	expectedKey := "sk-p...cdef"
	if redacted.APIKey != expectedKey {
		t.Errorf("redacted.APIKey = %q, want %q", redacted.APIKey, expectedKey)
	}

	// Verify other fields match original
	if redacted.Enabled != orig.Enabled ||
		redacted.BaseURL != orig.BaseURL ||
		redacted.Model != orig.Model ||
		redacted.MaxTokens != orig.MaxTokens {
		t.Errorf("redacted fields were modified unexpectedly: %+v", redacted)
	}
}

func TestRerankConfig_Redacted(t *testing.T) {
	orig := RerankConfig{
		Enabled: true,
		BaseURL: "http://localhost:8080",
		APIKey:  "shortkey",
		Model:   "bge-reranker-large",
		TopN:    10,
		Host:    "127.0.0.1",
	}

	redacted := orig.Redacted()

	// Verify original is untouched
	if orig.APIKey != "shortkey" {
		t.Errorf("original RerankConfig was mutated: APIKey = %q", orig.APIKey)
	}

	// Verify redacted key is masked (short key <= 8 chars)
	expectedKey := "********"
	if redacted.APIKey != expectedKey {
		t.Errorf("redacted.APIKey = %q, want %q", redacted.APIKey, expectedKey)
	}

	// Verify other fields match original
	if redacted.Enabled != orig.Enabled ||
		redacted.BaseURL != orig.BaseURL ||
		redacted.Model != orig.Model ||
		redacted.TopN != orig.TopN ||
		redacted.Host != orig.Host {
		t.Errorf("redacted fields were modified unexpectedly: %+v", redacted)
	}
}
