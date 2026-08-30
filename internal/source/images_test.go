package source

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestScanImages(t *testing.T) {
	dir := t.TempDir()

	// Valid image formats
	mustWrite(t, filepath.Join(dir, "photo.png"))
	mustWrite(t, filepath.Join(dir, "picture.jpg"))
	mustWrite(t, filepath.Join(dir, "image.jpeg"))
	mustWrite(t, filepath.Join(dir, "animation.gif"))
	mustWrite(t, filepath.Join(dir, "graphic.webp"))
	mustWrite(t, filepath.Join(dir, "bitmap.bmp"))
	mustWrite(t, filepath.Join(dir, "photo1.tiff"))
	mustWrite(t, filepath.Join(dir, "photo2.tif"))
	mustWrite(t, filepath.Join(dir, "vector.svg"))

	// Formats owned by other collections — must be excluded
	mustWrite(t, filepath.Join(dir, "readme.md"))
	mustWrite(t, filepath.Join(dir, "doc.pdf"))
	mustWrite(t, filepath.Join(dir, "report.docx"))

	// Unknown extension — excluded
	mustWrite(t, filepath.Join(dir, "archive.xyz"))

	// Nested directory — should be walked
	mustWrite(t, filepath.Join(dir, "sub", "deep.png"))

	files, err := ScanImages(dir)
	if err != nil {
		t.Fatalf("ScanImages: %v", err)
	}

	if got, want := len(files), 10; got != want {
		t.Fatalf("ScanImages = %d files, want %d", got, want)
	}

	seen := make(map[string]bool, len(files))
	for _, f := range files {
		if f.ContentHash == "" || f.Mtime == 0 || f.Name == "" {
			t.Errorf("file %s missing hash/mtime/name", f.Path)
		}
		seen[filepath.Base(f.Path)] = true
	}

	wantFiles := []string{
		"photo.png", "picture.jpg", "image.jpeg", "animation.gif",
		"graphic.webp", "bitmap.bmp", "photo1.tiff", "photo2.tif", "vector.svg", "deep.png",
	}
	for _, want := range wantFiles {
		if !seen[want] {
			t.Errorf("expected %s in scan results, not found", want)
		}
	}

	unwantedFiles := []string{"readme.md", "doc.pdf", "report.docx", "archive.xyz"}
	for _, unwanted := range unwantedFiles {
		if seen[unwanted] {
			t.Errorf("%s should NOT be in images scan", unwanted)
		}
	}
}

func TestScanImages_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "upper.PNG"))
	mustWrite(t, filepath.Join(dir, "mixed.Jpg"))

	files, err := ScanImages(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("ScanImages = %d, want 2 (case-insensitive ext)", len(files))
	}
}

func TestScanImages_EmptyDir(t *testing.T) {
	files, err := ScanImages(t.TempDir())
	if err != nil {
		t.Fatalf("ScanImages empty dir: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("ScanImages empty dir = %d files, want 0", len(files))
	}
}

func TestScanImages_ReadError(t *testing.T) {
	// Windows does not enforce POSIX read permissions — Chmod(0222) does not
	// prevent reading, so this test cannot verify read-error skipping on Windows.
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support POSIX read-only file permissions")
	}

	dir := t.TempDir()

	// Write a valid file
	validPath := filepath.Join(dir, "valid.png")
	mustWrite(t, validPath)

	// Write an invalid file (unreadable)
	invalidPath := filepath.Join(dir, "unreadable.png")
	mustWrite(t, invalidPath)
	// Change permissions to remove read access (write-only)
	if err := os.Chmod(invalidPath, 0222); err != nil {
		t.Skipf("skipping test due to inability to change file permissions: %v", err)
	}

	files, err := ScanImages(dir)
	if err != nil {
		t.Fatalf("ScanImages with read error: %v", err)
	}

	// Should only find the valid file, as the unreadable one is skipped
	if len(files) != 1 {
		t.Fatalf("ScanImages = %d files, want 1. Got: %v", len(files), files)
	}

	if filepath.Base(files[0].Path) != "valid.png" {
		t.Errorf("Expected valid.png, got %s", filepath.Base(files[0].Path))
	}
}
