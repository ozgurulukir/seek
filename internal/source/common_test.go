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
		name      string
		mediaType string
		want      string
	}{
		{name: "PNG", mediaType: "image/png", want: "png"},
		{name: "JPEG", mediaType: "image/jpeg", want: "jpg"},
		{name: "JPG", mediaType: "image/jpg", want: "jpg"},
		{name: "GIF", mediaType: "image/gif", want: "gif"},
		{name: "WEBP", mediaType: "image/webp", want: "webp"},
		{name: "SVG", mediaType: "image/svg+xml", want: "svg"},
		{name: "Uppercase PNG", mediaType: "IMAGE/PNG", want: "png"},
		{name: "Mixed case", mediaType: "iMaGe/JpEg", want: "jpg"},
		{name: "Unknown", mediaType: "application/json", want: "bin"},
		{name: "Empty", mediaType: "", want: "bin"},
		{name: "Malformed", mediaType: "image/", want: "bin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractExtension(tt.mediaType); got != tt.want {
				t.Errorf("ExtractExtension(%q) = %q; want %q", tt.mediaType, got, tt.want)
			}
		})
	}
}

func TestImageCacheDir(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get user home directory: %v", err)
	}

	expectedPath := filepath.Join(homeDir, ".cache", "seek", "images")
	actualPath := ImageCacheDir()

	if actualPath != expectedPath {
		t.Errorf("ImageCacheDir() returned %q, expected %q", actualPath, expectedPath)
	}
}
