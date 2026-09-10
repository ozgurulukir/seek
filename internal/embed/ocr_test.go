package embed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOCRClientExtractText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer testkey" {
			t.Errorf("auth = %q, want Bearer testkey", got)
		}
		var req struct {
			MaxTokens int  `json:"max_tokens"`
			Stream    bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.MaxTokens != 123 {
			t.Errorf("max_tokens = %d, want 123", req.MaxTokens)
		}
		if req.Stream {
			t.Error("stream = true, want false")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"EXTRACTED TEXT"}}]}`))
	}))
	defer srv.Close()

	c := NewOCRClient(srv.URL+"/", "testkey", "qwen-vl-ocr", 123)
	transport, ok := c.http.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("numeric loopback OCR client must bypass environment proxies")
	}
	got, err := c.ExtractText("data:image/png;base64,AAAA")
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if got != "EXTRACTED TEXT" {
		t.Errorf("got %q, want EXTRACTED TEXT", got)
	}
}

func TestOCRClientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := NewOCRClient(srv.URL, "k", "m", 0)
	if _, err := c.ExtractText("data:image/png;base64,AAAA"); err == nil {
		t.Fatal("expected error on 500, got nil")
	} else if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want status 500", err)
	}
}

func TestOCRClientDoesNotFollowRedirects(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected = true
	}))
	defer target.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()

	c := NewOCRClient(srv.URL, "local", "test-model", 0)
	if _, err := c.ExtractText("data:image/png;base64,AAAA"); err == nil {
		t.Fatal("expected redirect response to fail")
	}
	if redirected {
		t.Fatal("OCR client followed redirect to another host")
	}
}
