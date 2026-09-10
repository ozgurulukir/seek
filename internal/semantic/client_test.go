package semantic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
)

// TestClientTagRoundtrip verifies the envelope against a fake service.
func TestClientTagRoundtrip(t *testing.T) {
	var gotReq Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tag" {
			if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
				t.Fatalf("decode req: %v", err)
			}
			w.Write([]byte(`{"results":[{"id":0,"tags":["go","concurrency"],"topics":[{"label":"concurrency","score":0.8}],"entities":[{"text":"Go","type":"LOC"}]}],"errors":[],"corpus_lang":"en"}`))
			return
		}
		if r.URL.Path == "/health" {
			w.Write([]byte(`{"status":"ok","version":"0.1.0","models":{"lid":true,"ner":true,"keyphrase":true,"topic":false}}`))
			return
		}
		t.Fatalf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)

	h, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if h.Status != "ok" || !h.Models.Ner || h.Models.Topic {
		t.Fatalf("unexpected health: %+v", h)
	}

	resp, err := c.Tag(context.Background(), Request{Chunks: []Chunk{{ID: 0, Text: "Go concurrency"}}, MaxTags: 5})
	if err != nil {
		t.Fatalf("tag: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("want 1 result, got %d", len(resp.Results))
	}
	r := resp.Results[0]
	if r.ID != 0 || len(r.Tags) != 2 || r.Tags[0] != "go" || len(r.Topics) != 1 || len(r.Entities) != 1 {
		t.Fatalf("unexpected result: %+v", r)
	}
	if resp.CorpusLang != "en" {
		t.Fatalf("corpus_lang = %q, want en", resp.CorpusLang)
	}
	if gotReq.MaxTags != 5 {
		t.Fatalf("max_tags not forwarded: %+v", gotReq)
	}
}

// TestClientServiceDown returns a transport error when the service is down.
func TestClientServiceDown(t *testing.T) {
	// A closed port: the error must be a transport failure, not a panic.
	c := NewClient("http://127.0.0.1:1", 500*time.Millisecond)
	if _, err := c.Health(context.Background()); err == nil {
		t.Fatal("want error when service is down")
	}
}

// TestValidateOffline enforces the loopback-only rule under offline mode.
func TestValidateOffline(t *testing.T) {
	cases := []struct {
		name      string
		offline   bool
		url       string
		wantError bool
	}{
		{"offline loopback ok", true, "http://127.0.0.1:8003", false},
		{"offline ipv6 loopback ok", true, "http://[::1]:8003", false},
		{"offline remote refused", true, "http://nlp.example.com:8003", true},
		{"offline non-loopback ip refused", true, "http://10.0.0.5:8003", true},
		{"online remote allowed", false, "http://nlp.example.com:8003", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.AppConfig{}
			cfg.Config.Privacy.OfflineOnly = tc.offline
			err := ValidateOffline(cfg, tc.url)
			if (err != nil) != tc.wantError {
				t.Fatalf("ValidateOffline(%q, offline=%v) = %v, want error=%v", tc.url, tc.offline, err, tc.wantError)
			}
		})
	}
}

// TestNewClientFromConfigDisabled returns nil when the capability is off —
// the consumer must treat nil as "semantic is off", not an error.
func TestNewClientFromConfigDisabled(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.Config.Semantic.Enabled = false
	if got := NewClientFromConfig(cfg); got != nil {
		t.Fatalf("want nil client when disabled, got %T", got)
	}
	cfg.Config.Semantic.Enabled = true
	c := NewClientFromConfig(cfg)
	if c == nil {
		t.Fatal("want client when enabled")
	}
	if got := NewClientFromConfig(nil); got != nil {
		t.Fatalf("want nil client for nil config, got %T", got)
	}
}

// TestEffectiveConfig clamping.
func TestEffectiveConfig(t *testing.T) {
	s := config.SemanticConfig{}
	if got := s.EffectiveBaseURL(); got != config.DefaultSemanticBaseURL {
		t.Fatalf("default base url = %q", got)
	}
	if got := s.EffectiveMaxTags(); got != config.DefaultSemanticMaxTags {
		t.Fatalf("default max tags = %d", got)
	}
	if got := s.EffectiveTimeout(); got != config.DefaultSemanticTimeout {
		t.Fatalf("default timeout = %v", got)
	}

	s = config.SemanticConfig{MaxTags: 999, BaseURL: "http://127.0.0.1:9999", Timeout: time.Second}
	if got := s.EffectiveMaxTags(); got != config.DefaultSemanticMaxTags {
		t.Fatalf("max tags should clamp to %d, got %d", config.DefaultSemanticMaxTags, got)
	}
	if got := s.EffectiveBaseURL(); !strings.Contains(got, "9999") {
		t.Fatalf("configured base url not used: %q", got)
	}
}
