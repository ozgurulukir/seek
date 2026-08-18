package source_test

import (
	"os"
	"path/filepath"

	"testing"

	"github.com/ozgurulukir/seek/internal/source"
)

func TestImageCacheDir(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get user home directory: %v", err)
	}

	expectedPath := filepath.Join(homeDir, ".cache", "seek", "images")
	actualPath := source.ImageCacheDir()

	if actualPath != expectedPath {
		t.Errorf("ImageCacheDir() returned %q, expected %q", actualPath, expectedPath)
	}
}
