package cmd

import (
	"errors"
	"testing"
)

func TestMaskKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Empty stays empty: rendering an unset key must not imply a
		// configured (masked) credential.
		{"", ""},
		{"12345678", "********"},
		{"sk-1234567890abcdef", "sk-1...cdef"},
	}

	for _, tt := range tests {
		got := maskKey(tt.input)
		if got != tt.want {
			t.Errorf("maskKey(%q) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsLocalEndpoint(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"http://localhost:11434/v1", true},
		{"http://127.0.0.1:8000", true},
		{"http://[::1]:11434", true},
		{"https://api.openai.com/v1", false},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", false},
	}

	for _, tt := range tests {
		got := isLocalEndpoint(tt.url)
		if got != tt.want {
			t.Errorf("isLocalEndpoint(%q) = %v; want %v", tt.url, got, tt.want)
		}
	}
}

func TestProvidersConfig(t *testing.T) {
	if len(providers) < 4 {
		t.Fatalf("expected at least 4 providers, got %d", len(providers))
	}

	// Verify Ollama exists with 768 dimensions
	foundOllama := false
	for _, p := range providers {
		if p.Model == "nomic-embed-text" {
			foundOllama = true
			if p.Dimensions != 768 {
				t.Errorf("expected nomic-embed-text to have 768 dimensions, got %d", p.Dimensions)
			}
		}
	}
	if !foundOllama {
		t.Errorf("expected nomic-embed-text in providers")
	}
}

// TestInputErrPropagates pins the M16 contract: interactive input failures
// must surface as command errors (non-zero exit) instead of `return nil` —
// an aborted `seek auth login` used to look like success to automation
// (review 2026-09-17 M16).
func TestInputErrPropagates(t *testing.T) {
	if !errors.Is(inputErr(errCanceled), errCanceled) {
		t.Error("cancel must propagate as errCanceled")
	}
	boom := errors.New("stdin closed")
	if !errors.Is(inputErr(boom), boom) {
		t.Error("read errors must propagate wrapped, not vanish")
	}
}
