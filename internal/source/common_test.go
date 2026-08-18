package source

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveImage(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		data      []byte
		mediaType string
		subPath   string
		wantErr   bool
	}{
		{
			name:      "Save valid PNG image",
			data:      []byte("fake-png-data"),
			mediaType: "image/png",
			subPath:   "test1/image.png",
			wantErr:   false,
		},
		{
			name:      "Save valid JPEG image",
			data:      []byte("fake-jpeg-data"),
			mediaType: "image/jpeg",
			subPath:   "test2/image.jpg",
			wantErr:   false,
		},
		{
			name:      "Save with deep directory structure",
			data:      []byte("fake-data"),
			mediaType: "image/webp",
			subPath:   "deep/nested/dir/image.webp",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, tt.subPath)

			err := SaveImage(tt.data, tt.mediaType, path)

			if (err != nil) != tt.wantErr {
				t.Errorf("SaveImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				savedData, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("Failed to read saved file: %v", err)
				}

				if !bytes.Equal(savedData, tt.data) {
					t.Errorf("Saved data mismatch. Got %v, want %v", savedData, tt.data)
				}
			}
		})
	}
}

func TestSaveImage_DirCreateError(t *testing.T) {
	tmpDir := t.TempDir()

	conflictPath := filepath.Join(tmpDir, "conflict")
	if err := os.WriteFile(conflictPath, []byte("conflict file"), 0644); err != nil {
		t.Fatalf("Failed to create conflict file: %v", err)
	}

	path := filepath.Join(conflictPath, "image.png")

	err := SaveImage([]byte("data"), "image/png", path)
	if err == nil {
		t.Error("Expected error when trying to create dir over a file, got nil")
	}
}

func TestExtractExtension(t *testing.T) {
	tests := []struct {
		mediaType string
		want      string
	}{
		{"image/png", "png"},
		{"IMAGE/PNG", "png"}, // Test case-insensitivity
		{"image/jpeg", "jpg"},
		{"image/jpg", "jpg"},
		{"image/gif", "gif"},
		{"image/webp", "webp"},
		{"image/svg+xml", "svg"},
		{"application/pdf", "bin"}, // Unknown type fallback
		{"", "bin"},                // Empty string fallback
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			if got := ExtractExtension(tt.mediaType); got != tt.want {
				t.Errorf("ExtractExtension(%q) = %v, want %v", tt.mediaType, got, tt.want)
			}
		})
	}
}

func TestImageCacheDir(t *testing.T) {
	dir := ImageCacheDir()
	if dir == "" {
		t.Error("ImageCacheDir returned empty string")
	}

	// Ensure it has the expected path suffix
	expectedSuffix := filepath.Join(".cache", "seek", "images")
	if len(dir) < len(expectedSuffix) || dir[len(dir)-len(expectedSuffix):] != expectedSuffix {
		t.Errorf("ImageCacheDir() = %q, want suffix %q", dir, expectedSuffix)
	}
}
