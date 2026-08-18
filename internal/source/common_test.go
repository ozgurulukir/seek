package source

import (
	"testing"
)

func TestExtractExtension(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		expected  string
	}{
		{
			name:      "png",
			mediaType: "image/png",
			expected:  "png",
		},
		{
			name:      "jpeg",
			mediaType: "image/jpeg",
			expected:  "jpg",
		},
		{
			name:      "jpg",
			mediaType: "image/jpg",
			expected:  "jpg",
		},
		{
			name:      "gif",
			mediaType: "image/gif",
			expected:  "gif",
		},
		{
			name:      "webp",
			mediaType: "image/webp",
			expected:  "webp",
		},
		{
			name:      "svg",
			mediaType: "image/svg+xml",
			expected:  "svg",
		},
		{
			name:      "empty string",
			mediaType: "",
			expected:  "bin",
		},
		{
			name:      "unknown media type",
			mediaType: "application/json",
			expected:  "bin",
		},
		{
			name:      "case insensitivity",
			mediaType: "IMAGE/PNG",
			expected:  "png",
		},
		{
			name:      "mixed case",
			mediaType: "iMaGe/JpEg",
			expected:  "jpg",
		},
		{
			name:      "malformed string",
			mediaType: "image/",
			expected:  "bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractExtension(tt.mediaType); got != tt.expected {
				t.Errorf("ExtractExtension(%q) = %v, want %v", tt.mediaType, got, tt.expected)
			}
		})
	}
}
