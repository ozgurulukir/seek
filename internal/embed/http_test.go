package embed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPostJSONResponseSizeLimit verifies the success-path response cap: an
// endpoint answering with a body larger than maxResponseBody must fail with an
// explicit limit error instead of being fully buffered into memory.
func TestPostJSONResponseSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Stream slightly over the cap so LimitReader+1 detects overflow.
		chunk := make([]byte, 1<<20)
		for i := range chunk {
			chunk[i] = ' '
		}
		total := maxResponseBody + 1<<10
		for written := 0; written < total; written += len(chunk) {
			_, _ = w.Write(chunk)
		}
	}))
	defer server.Close()

	var resp struct{}
	err := postJSON(context.Background(), server.Client(), server.URL, "", "embedding", map[string]string{"m": "x"}, &resp)
	if err == nil {
		t.Fatal("expected error for oversized response body")
	}
	want := fmt.Sprintf("embedding API response exceeds %d MiB limit", maxResponseBody>>20)
	if fmt.Sprint(err) != want {
		t.Fatalf("unexpected error: %v (want %q)", err, want)
	}
}

// TestPostJSONNormalResponse confirms the cap leaves normal-size responses
// untouched.
func TestPostJSONNormalResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var resp struct {
		OK bool `json:"ok"`
	}
	if err := postJSON(context.Background(), server.Client(), server.URL, "", "embedding", map[string]string{"m": "x"}, &resp); err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	if !resp.OK {
		t.Fatal("response not parsed")
	}
}
