package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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

// fakeOCR is an extractor.OCR that returns a canned string.
type fakeOCR struct{ out string }

func (f fakeOCR) ExtractText(string) (string, error) { return f.out, nil }

func TestName(t *testing.T) {
	e := New(nil, t.TempDir())
	if e.Name() != "builtin" {
		t.Errorf("Name = %q, want builtin", e.Name())
	}
}

func TestSupports(t *testing.T) {
	e := New(nil, t.TempDir())
	cases := map[string]bool{
		"readme.md": true, "doc.markdown": true, "report.pdf": true,
		"photo.png": true, "img.jpeg": true, "icon.svg": true,
		"report.docx": false, "data.xlsx": false, "page.html": false,
		"unknown.xyz": false,
	}
	for path, want := range cases {
		if got := e.Supports(path); got != want {
			t.Errorf("Supports(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestExtractMarkdown(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "note.md")
	if err := os.WriteFile(p, []byte("# My Title\n\nbody"), 0644); err != nil {
		t.Fatal(err)
	}
	e := New(nil, t.TempDir())
	res, err := e.Extract(context.Background(), p)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if res.Title != "My Title" {
		t.Errorf("Title = %q, want My Title", res.Title)
	}
	if res.Content != "# My Title\n\nbody" {
		t.Errorf("Content = %q", res.Content)
	}
	if res.MimeType != "text/markdown" {
		t.Errorf("MimeType = %q", res.MimeType)
	}
}

func TestExtractMarkdown_TitleFallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "noheading.md")
	if err := os.WriteFile(p, []byte("just body, no heading"), 0644); err != nil {
		t.Fatal(err)
	}
	e := New(nil, t.TempDir())
	res, _ := e.Extract(context.Background(), p)
	if res.Title != "noheading" {
		t.Errorf("Title = %q, want noheading (filename fallback)", res.Title)
	}
}

func TestExtractPDF_PagesRendered(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	writeMinPdf(t, pdfPath)

	e := New(nil, t.TempDir())
	res, err := e.Extract(context.Background(), pdfPath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("Pages = %d, want 1", len(res.Pages))
	}
	if res.Pages[0].Seq != 0 {
		t.Errorf("page seq = %d, want 0", res.Pages[0].Seq)
	}
	if _, err := os.Stat(res.Pages[0].Path); err != nil {
		t.Errorf("cached png not written: %v", err)
	}
	if res.MimeType != "application/pdf" {
		t.Errorf("MimeType = %q", res.MimeType)
	}
}

func TestExtractPDF_DeterministicCache(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	writeMinPdf(t, pdfPath)

	e := New(nil, t.TempDir())
	res1, err := e.Extract(context.Background(), pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	res2, err := e.Extract(context.Background(), pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if res1.Pages[0].Path != res2.Pages[0].Path {
		t.Errorf("cache not keyed by hash: %s vs %s", res2.Pages[0].Path, res1.Pages[0].Path)
	}
}

func TestExtractPDF_OCRInvoked(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "scan.pdf")
	writeMinPdf(t, pdfPath) // minimal PDF has no text layer

	e := New(fakeOCR{out: "SCANNED PAGE TEXT"}, t.TempDir())
	res, err := e.Extract(context.Background(), pdfPath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("Pages = %d, want 1", len(res.Pages))
	}
	if res.Pages[0].Text != "SCANNED PAGE TEXT" {
		t.Errorf("Text = %q, want OCR output", res.Pages[0].Text)
	}
	if !contains(res.Content, "SCANNED PAGE TEXT") {
		t.Errorf("Content = %q, want to contain OCR output", res.Content)
	}
}

func TestExtractPDF_NoOCRWhenNil(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "scan.pdf")
	writeMinPdf(t, pdfPath)

	e := New(nil, t.TempDir()) // ocr == nil
	res, err := e.Extract(context.Background(), pdfPath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if res.Pages[0].Text != "" {
		t.Errorf("Text = %q, want empty (no OCR)", res.Pages[0].Text)
	}
	if res.Content != "" {
		t.Errorf("Content = %q, want empty (no text extracted)", res.Content)
	}
}

func TestExtractImage_PassThrough(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(p, []byte("fake png bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	e := New(nil, t.TempDir())
	res, err := e.Extract(context.Background(), p)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if res.Content != "" {
		t.Errorf("Content = %q, want empty (pass-through)", res.Content)
	}
	if res.MimeType != "image/png" {
		t.Errorf("MimeType = %q, want image/png", res.MimeType)
	}
	if res.Title != "photo" {
		t.Errorf("Title = %q, want photo", res.Title)
	}
}

func TestExtract_Unsupported(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "doc.docx")
	if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	e := New(nil, t.TempDir())
	_, err := e.Extract(context.Background(), p)
	if err == nil {
		t.Fatal("Extract unsupported: expected error, got nil")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
