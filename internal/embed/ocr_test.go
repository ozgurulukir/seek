package embed

import (
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
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"EXTRACTED TEXT"}}]}`))
	}))
	defer srv.Close()

	c := NewOCRClient(srv.URL, "testkey", "qwen-vl-ocr")
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

	c := NewOCRClient(srv.URL, "k", "m")
	if _, err := c.ExtractText("data:image/png;base64,AAAA"); err == nil {
		t.Fatal("expected error on 500, got nil")
	} else if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want status 500", err)
	}
}
