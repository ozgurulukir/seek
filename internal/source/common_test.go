package source

import (
	"testing"
)

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
		{name: "Unknown", mediaType: "application/json", want: "bin"},
		{name: "Empty", mediaType: "", want: "bin"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractExtension(tc.mediaType)
			if got != tc.want {
				t.Errorf("ExtractExtension(%q) = %q; want %q", tc.mediaType, got, tc.want)
			}
		})
	}
}
