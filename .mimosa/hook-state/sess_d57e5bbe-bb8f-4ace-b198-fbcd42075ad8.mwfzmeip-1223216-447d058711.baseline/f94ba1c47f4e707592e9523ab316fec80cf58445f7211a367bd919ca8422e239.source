package source

import (
	"os"
	"path/filepath"
	"runtime"
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

	files, issues, err := ScanPdfs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("ScanPdfs = %d issues for normal dir, want 0", len(issues))
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

func TestScanPdfs_ReadError(t *testing.T) {
	// Windows does not enforce POSIX read permissions — Chmod(0222) does not
	// prevent reading, so this test cannot verify read-error skipping on Windows.
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support POSIX read-only file permissions")
	}

	dir := t.TempDir()
	writeMinPdf(t, filepath.Join(dir, "valid.pdf"))

	unreadable := filepath.Join(dir, "unreadable.pdf")
	writeMinPdf(t, unreadable)
	if err := os.Chmod(unreadable, 0222); err != nil {
		t.Skipf("skipping test due to inability to change file permissions: %v", err)
	}

	files, issues, err := ScanPdfs(dir)
	if err != nil {
		t.Fatalf("ScanPdfs with read error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("ScanPdfs = %d files, want 1. Got: %v", len(files), files)
	}
	if filepath.Base(files[0].Path) != "valid.pdf" {
		t.Errorf("Expected valid.pdf, got %s", filepath.Base(files[0].Path))
	}

	// The unreadable file's I/O error must be surfaced as a scan issue.
	if len(issues) != 1 {
		t.Fatalf("ScanPdfs = %d issues, want 1 (the unreadable file)", len(issues))
	}
	if issues[0].Err == nil {
		t.Errorf("scan issue for %s has nil error", issues[0].Path)
	}
	if filepath.Base(issues[0].Path) != "unreadable.pdf" {
		t.Errorf("expected issue for unreadable.pdf, got %s", filepath.Base(issues[0].Path))
	}
}
