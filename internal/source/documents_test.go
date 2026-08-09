package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanDocuments(t *testing.T) {
	dir := t.TempDir()
	// Rich formats that the documents scanner should pick up.
	mustWrite(t, filepath.Join(dir, "report.docx"))
	mustWrite(t, filepath.Join(dir, "budget.xlsx"))
	mustWrite(t, filepath.Join(dir, "slides.pptx"))
	mustWrite(t, filepath.Join(dir, "book.epub"))
	mustWrite(t, filepath.Join(dir, "page.html"))
	mustWrite(t, filepath.Join(dir, "notes.json"))
	mustWrite(t, filepath.Join(dir, "export.csv"))
	// Formats owned by other collection types — must be excluded.
	mustWrite(t, filepath.Join(dir, "readme.md"))
	mustWrite(t, filepath.Join(dir, "doc.pdf"))
	mustWrite(t, filepath.Join(dir, "photo.png"))
	// Unknown extension — excluded.
	mustWrite(t, filepath.Join(dir, "archive.xyz"))
	// Nested directory — should be walked.
	mustWrite(t, filepath.Join(dir, "sub", "deep.docx"))

	files, err := ScanDocuments(dir)
	if err != nil {
		t.Fatalf("ScanDocuments: %v", err)
	}
	if got, want := len(files), 8; got != want {
		t.Fatalf("ScanDocuments = %d files, want %d", got, want)
	}
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		if f.ContentHash == "" || f.Mtime == 0 || f.Name == "" {
			t.Errorf("file %s missing hash/mtime/name", f.Path)
		}
		seen[filepath.Base(f.Path)] = true
	}
	for _, want := range []string{"report.docx", "budget.xlsx", "slides.pptx", "book.epub", "page.html", "notes.json", "export.csv", "deep.docx"} {
		if !seen[want] {
			t.Errorf("expected %s in scan results, not found", want)
		}
	}
	for _, unwanted := range []string{"readme.md", "doc.pdf", "photo.png", "archive.xyz"} {
		if seen[unwanted] {
			t.Errorf("%s should NOT be in documents scan (owned by another type)", unwanted)
		}
	}
}

func TestScanDocuments_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "upper.DOCX"))
	mustWrite(t, filepath.Join(dir, "mixed.Docx"))

	files, err := ScanDocuments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("ScanDocuments = %d, want 2 (case-insensitive ext)", len(files))
	}
}

func TestScanDocuments_EmptyDir(t *testing.T) {
	files, err := ScanDocuments(t.TempDir())
	if err != nil {
		t.Fatalf("ScanDocuments empty dir: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("ScanDocuments empty dir = %d files, want 0", len(files))
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
}
