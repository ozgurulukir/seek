package embed

import "testing"

func TestNewVLClientEndpoint(t *testing.T) {
	// Explicit endpoint is used as-is.
	c := NewVLClient("k", "model", 8, "https://provider.example/v1/embeddings")
	if c.endpoint != "https://provider.example/v1/embeddings" {
		t.Errorf("explicit endpoint = %q, want custom", c.endpoint)
	}

	// Empty endpoint falls back to DashScope default.
	d := NewVLClient("k", "model", 8, "")
	if d.endpoint != DefaultVLEndpoint {
		t.Errorf("empty endpoint = %q, want %q", d.endpoint, DefaultVLEndpoint)
	}
}
