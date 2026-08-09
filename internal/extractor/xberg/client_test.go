package xberg

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
)

// newTestClient builds a Client pointing at the given test server, with the
// /formats cache pre-seeded so Supports doesn't try to hit the network.
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := New(config.ExtractorConfig{
		Backend:      "xberg",
		XbergBaseURL: baseURL,
		OutputFormat: "markdown",
		Timeout:      0, // use default
	}, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Seed the formats cache so Supports works without a live /formats call.
	c.formatsMu.Lock()
	c.formats = map[string]bool{".docx": true, ".xlsx": true, ".pdf": true}
	c.fetched = true
	c.formatsMu.Unlock()
	return c
}

// writeDoc creates a temp file named name with dummy content so Extract's
// os.Open succeeds; the bytes never reach the (fake) server's parser.
func writeDoc(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("dummy document bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeServer returns an httptest.Server that handles /health, /formats, and
// /extract. extractHandler receives the parsed multipart fields for assertions.
func fakeServer(t *testing.T, extractHandler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
	})
	mux.HandleFunc("/formats", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]supportedFormat{
			{Extension: ".docx", MimeType: "application/vnd.openxmlformats"},
			{Extension: ".xlsx", MimeType: "application/vnd.ms-excel"},
		})
	})
	mux.HandleFunc("/extract", extractHandler)
	return httptest.NewServer(mux)
}

func TestExtract_Success(t *testing.T) {
	var gotOutputFormat, gotFilename string
	srv := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotOutputFormat = r.MultipartForm.Value["output_format"][0]
		fh := r.MultipartForm.File["file"][0]
		gotFilename = fh.Filename
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(extractionResult{
			Results: []extractedDocument{{Content: "# Title\n\nbody text", MimeType: "text/markdown"}},
		})
	})
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	res, err := c.Extract(context.Background(), writeDoc(t, "report.docx"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if gotOutputFormat != "markdown" {
		t.Errorf("output_format = %q, want markdown", gotOutputFormat)
	}
	if gotFilename != "report.docx" {
		t.Errorf("filename = %q, want report.docx", gotFilename)
	}
	if res.Content != "# Title\n\nbody text" {
		t.Errorf("Content = %q", res.Content)
	}
	if res.Title != "report" {
		t.Errorf("Title = %q, want report", res.Title)
	}
}

func TestExtract_ServerError(t *testing.T) {
	srv := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.Extract(context.Background(), writeDoc(t, "report.docx"))
	if err == nil {
		t.Fatal("Extract: expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error = %v, want status 500", err)
	}
}

func TestExtract_EmptyContent(t *testing.T) {
	srv := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(extractionResult{
			Results: []extractedDocument{{Content: "   ", MimeType: "text/markdown"}},
		})
	})
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.Extract(context.Background(), writeDoc(t, "report.docx"))
	if err == nil || !strings.Contains(err.Error(), "empty content") {
		t.Fatalf("Extract with empty content: err = %v, want empty content error", err)
	}
}

func TestExtract_NoResults_WithErrors(t *testing.T) {
	srv := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(extractionResult{
			Results: []extractedDocument{},
			Errors:  []extractionError{{Input: "report.docx", Message: "encrypted pdf"}},
		})
	})
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.Extract(context.Background(), writeDoc(t, "report.docx"))
	if err == nil || !strings.Contains(err.Error(), "encrypted pdf") {
		t.Fatalf("err = %v, want encrypted pdf message", err)
	}
}

func TestSupports_UsesCachedFormats(t *testing.T) {
	c := newTestClient(t, "http://unused.invalid") // no server; cache pre-seeded
	if !c.Supports("a.docx") {
		t.Error("Supports(a.docx) = false, want true (cached)")
	}
	if c.Supports("a.unknown") {
		t.Error("Supports(a.unknown) = true, want false")
	}
}

func TestSupports_NoExtension(t *testing.T) {
	c := newTestClient(t, "http://unused.invalid")
	if c.Supports("README") {
		t.Error("Supports(README) = true, want false (no extension)")
	}
}

func TestHealth_Unreachable(t *testing.T) {
	c, _ := New(config.ExtractorConfig{Backend: "xberg", XbergBaseURL: "http://127.0.0.1:1"}, t.TempDir())
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("Health on unreachable server: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "health check") {
		t.Errorf("err = %v, want health check message", err)
	}
}

func TestFormats_FetchedOnce(t *testing.T) {
	calls := 0
	srv := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode([]supportedFormat{{Extension: ".docx"}})
	})
	// Override /formats counter via a dedicated handler: reset the fakeServer.
	srv.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
	})
	mux.HandleFunc("/formats", func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode([]supportedFormat{{Extension: ".docx"}})
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c, err := New(config.ExtractorConfig{Backend: "xberg", XbergBaseURL: srv.URL}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// First Supports triggers a fetch.
	if !c.Supports("a.docx") {
		t.Fatal("first Supports = false")
	}
	// Second Supports reuses the cache; formats should be fetched only once.
	if !c.Supports("b.docx") {
		t.Fatal("second Supports = false")
	}
	if calls != 1 {
		t.Errorf("formats fetched %d times, want 1", calls)
	}
}
