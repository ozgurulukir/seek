package source

import (
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

func TestScanPdfs(t *testing.T) {
	dir := t.TempDir()
	writeMinPdf(t, filepath.Join(dir, "a.pdf"))
	os.WriteFile(filepath.Join(dir, "note.md"), []byte("# hi"), 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	writeMinPdf(t, filepath.Join(dir, "sub", "b.PDF")) // uppercase ext

	files, err := ScanPdfs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("ScanPdfs = %d files, want 2 (only .pdf, case-insensitive)", len(files))
	}
	for _, f := range files {
		if f.ContentHash == "" || f.Mtime == 0 {
			t.Errorf("file %s missing hash/mtime", f.Path)
		}
	}
}

func TestRasterizePDF(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	writeMinPdf(t, pdfPath)

	pages, err := RasterizePDF(pdfPath, dir, 72, nil)
	if err != nil {
		t.Fatalf("RasterizePDF: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(pages))
	}
	if pages[0].Seq != 0 {
		t.Errorf("seq = %d, want 0", pages[0].Seq)
	}
	if _, err := os.Stat(pages[0].Path); err != nil {
		t.Errorf("cached png not written: %v", err)
	}
	// Deterministic cache dir keyed by content hash.
	pages2, err := RasterizePDF(pdfPath, dir, 72, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pages2[0].Path != pages[0].Path {
		t.Errorf("cache not keyed by hash: %s vs %s", pages2[0].Path, pages[0].Path)
	}
}

// fakeExtractor is a TextExtractor that returns a canned string.
type fakeExtractor struct{ out string }

func (f fakeExtractor) ExtractText(string) (string, error) { return f.out, nil }

// TestRasterizePDFOCR verifies OCR is invoked when a page has no embedded text.
func TestRasterizePDFOCR(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "scan.pdf")
	writeMinPdf(t, pdfPath) // minimal PDF has no text layer

	ocr := fakeExtractor{out: "SCANNED PAGE TEXT"}
	pages, err := RasterizePDF(pdfPath, dir, 72, ocr)
	if err != nil {
		t.Fatalf("RasterizePDF: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(pages))
	}
	if pages[0].Text != "SCANNED PAGE TEXT" {
		t.Errorf("Text = %q, want OCR output", pages[0].Text)
	}
}

// TestRasterizePDFNoOCR verifies no OCR is attempted when extractor is nil.
func TestRasterizePDFNoOCR(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "scan.pdf")
	writeMinPdf(t, pdfPath)

	pages, err := RasterizePDF(pdfPath, dir, 72, nil)
	if err != nil {
		t.Fatalf("RasterizePDF: %v", err)
	}
	if pages[0].Text != "" {
		t.Errorf("Text = %q, want empty (no OCR)", pages[0].Text)
	}
}
