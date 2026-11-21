package config

import "testing"

func TestProviderKindString(t *testing.T) {
	if ProviderKindHosted.String() != "hosted" {
		t.Errorf("unexpected String(): %q", ProviderKindHosted.String())
	}
}

func providerKindTestConfig(baseURL, model string) *AppConfig {
	return &AppConfig{Config: Config{
		Embedding: EmbeddingConfig{BaseURL: baseURL, Model: model},
	}}
}

func TestIsLocalEndpoint(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"http://127.0.0.1:8080/v1", true},
		{"http://[::1]:11434/v1", true},
		{"http://localhost:11434/v1", true},
		{"http://LOCALHOST:11434/v1", true},
		{"http://10.0.0.1:8080/v1", false},
		{"http://example.com/v1", false},
		// Hostnames other than localhost are deliberately not local: a
		// hosts-file entry must not turn a remote destination into a trusted
		// local one.
		{"http://localhost.example.com/v1", false},
		{"::1", false},
	}
	for _, tt := range tests {
		if got := isLocalEndpoint(tt.url); got != tt.want {
			t.Errorf("isLocalEndpoint(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestDetectProviderKind(t *testing.T) {
	tests := []struct {
		name string
		cfg  *AppConfig
		want ProviderKind
	}{
		{"nil config", nil, ProviderKindGeneric},
		{"empty url", providerKindTestConfig("", "m"), ProviderKindGeneric},
		{"dashscope exact", providerKindTestConfig("https://dashscope.aliyuncs.com/compatible-mode/v1", "text-embedding"), ProviderKindHosted},
		{"dashscope subdomain", providerKindTestConfig("https://api.dashscope.aliyuncs.com/compatible-mode/v1", "text-embedding"), ProviderKindHosted},
		// Lookalike hosts must not classify as hosted.
		{"dashscope lookalike", providerKindTestConfig("https://evil-dashscope.aliyuncs.com/v1", "text-embedding"), ProviderKindGeneric},
		{"openai", providerKindTestConfig("https://api.openai.com/v1", "text-embedding-3-small"), ProviderKindHosted},
		{"ollama port", providerKindTestConfig("http://127.0.0.1:11434/v1", "nomic-embed-text"), ProviderKindOllama},
		{"ollama model", providerKindTestConfig("http://localhost:8080/v1", "ollama-embed"), ProviderKindOllama},
		{"local helper", providerKindTestConfig("http://127.0.0.1:9090/v1", "fembed"), ProviderKindLocal},
		{"generic remote", providerKindTestConfig("https://embed.example.com/v1", "text-embedding"), ProviderKindGeneric},
	}
	for _, tt := range tests {
		if got := DetectProviderKind(tt.cfg); got != tt.want {
			t.Errorf("%s: DetectProviderKind() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
