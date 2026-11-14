package indexer_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
)

// writeMinPdf writes a minimal valid single-page PDF to path.
func writeMinPdf(t *testing.T, path string) {
	t.Helper()
	pdf := []byte("%PDF-1.4\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n" +
		"2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n" +
		"3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R >> endobj\n" +
		"4 0 obj << /Length 10 >> stream\nBT ET\nendstream\nendobj\n" +
		"xref\n0 5\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n0000000202 00000 n \n" +
		"trailer << /Size 5 /Root 1 0 R >>\nstartxref\n270\n%%EOF\n")
	if err := os.WriteFile(path, pdf, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestNewExtractor_BuiltinPDFExtractionIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")

	cfg := &config.AppConfig{
		CacheDir: cacheDir,
	}

	// 1. Instantiate via factory
	ext, err := indexer.NewExtractor(cfg, "builtin")
	if err != nil {
		t.Fatalf("NewExtractor(builtin): %v", err)
	}
	if ext == nil {
		t.Fatal("expected non-nil Extractor")
	}
	if ext.Name() != "builtin" {
		t.Errorf("ext.Name() = %q, want 'builtin'", ext.Name())
	}

	// 2. Write minimal PDF
	pdfPath := filepath.Join(tmpDir, "test_doc.pdf")
	writeMinPdf(t, pdfPath)

	if !ext.Supports(pdfPath) {
		t.Fatalf("ext.Supports(%q) = false, want true", pdfPath)
	}

	// 3. Extract PDF pages
	doc, err := ext.Extract(context.Background(), pdfPath)
	if err != nil {
		t.Fatalf("ext.Extract: %v", err)
	}

	if doc.MimeType != "application/pdf" {
		t.Errorf("doc.MimeType = %q, want application/pdf", doc.MimeType)
	}

	if len(doc.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(doc.Pages))
	}

	page := doc.Pages[0]
	if page.Seq != 0 {
		t.Errorf("page.Seq = %d, want 0", page.Seq)
	}

	if _, err := os.Stat(page.Path); err != nil {
		t.Errorf("rendered page image does not exist at %q: %v", page.Path, err)
	}

	// 4. Test unknown backend fails fast
	_, err = indexer.NewExtractor(cfg, "unknown-backend")
	if err == nil {
		t.Error("expected error for unknown backend, got nil")
	}
}

func TestNewExtractor_AllowsLoopbackOCRWhenOfflineOnly(t *testing.T) {
	ocrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"LOCAL OCR TEXT"}}]}`)
	}))
	t.Cleanup(ocrServer.Close)

	tmpDir := t.TempDir()
	cfg := &config.AppConfig{CacheDir: filepath.Join(tmpDir, "cache")}
	cfg.Config.OCR = config.OCRConfig{
		Enabled: true,
		BaseURL: ocrServer.URL,
		APIKey:  "local",
		Model:   "test-ocr",
	}
	cfg.Config.Privacy.OfflineOnly = true

	ext, err := indexer.NewExtractor(cfg, "builtin")
	if err != nil {
		t.Fatalf("NewExtractor(builtin): %v", err)
	}

	pdfPath := filepath.Join(tmpDir, "scan.pdf")
	writeMinPdf(t, pdfPath)
	doc, err := ext.Extract(context.Background(), pdfPath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(doc.Pages) != 1 {
		t.Fatalf("Pages = %d, want 1", len(doc.Pages))
	}
	if got := doc.Pages[0].Text; got != "LOCAL OCR TEXT" {
		t.Errorf("OCR text = %q, want %q", got, "LOCAL OCR TEXT")
	}
}
