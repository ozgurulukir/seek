package embed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestReadLimitedBody exercises the bounded-read helper with a small limit so
// the overflow branch is cheap to test (the production limit is 256 MiB).
func TestReadLimitedBody(t *testing.T) {
	t.Run("within limit", func(t *testing.T) {
		got, err := readLimitedBody(strings.NewReader("hello"), 8, "download")
		if err != nil {
			t.Fatalf("readLimitedBody: %v", err)
		}
		if string(got) != "hello" {
			t.Errorf("body = %q, want hello", got)
		}
	})

	t.Run("exactly at limit", func(t *testing.T) {
		got, err := readLimitedBody(strings.NewReader("12345678"), 8, "download")
		if err != nil {
			t.Fatalf("readLimitedBody: %v", err)
		}
		if len(got) != 8 {
			t.Errorf("len = %d, want 8", len(got))
		}
	})

	t.Run("over limit", func(t *testing.T) {
		_, err := readLimitedBody(strings.NewReader("123456789"), 8, "download")
		if err == nil {
			t.Fatal("expected error for oversized body")
		}
		if !strings.Contains(err.Error(), "download response exceeds 8 byte limit") {
			t.Errorf("error = %q, want clear limit message", err.Error())
		}
	})
}
