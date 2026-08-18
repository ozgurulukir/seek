package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanMarkdown(t *testing.T) {
	dir := t.TempDir()

	// Write markdown files
	mustWriteContent(t, filepath.Join(dir, "readme.md"), "# Readme\nHello world\n")
	mustWriteContent(t, filepath.Join(dir, "notes.markdown"), "## Subtitle\nNo main title here\n")
	mustWriteContent(t, filepath.Join(dir, "sub", "deep.md"), "# Deep File\nNested file\n")

	// Write non-markdown files (should be ignored)
	mustWriteContent(t, filepath.Join(dir, "image.png"), "fake image")
	mustWriteContent(t, filepath.Join(dir, "document.pdf"), "fake pdf")
	mustWriteContent(t, filepath.Join(dir, "source.go"), "package main\n")

	files, err := ScanMarkdown(dir, "")
	if err != nil {
		t.Fatalf("ScanMarkdown failed: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("ScanMarkdown returned %d files, want 3", len(files))
	}

	// Verify details
	fileMap := make(map[string]FileInfo)
	for _, f := range files {
		fileMap[filepath.Base(f.Path)] = f
	}

	if f, ok := fileMap["readme.md"]; !ok {
		t.Errorf("readme.md missing")
	} else if f.Title != "Readme" {
		t.Errorf("readme.md title = %q, want %q", f.Title, "Readme")
	}

	if f, ok := fileMap["notes.markdown"]; !ok {
		t.Errorf("notes.markdown missing")
	} else if f.Title != "notes" {
		t.Errorf("notes.markdown title = %q, want %q", f.Title, "notes")
	}

	if f, ok := fileMap["deep.md"]; !ok {
		t.Errorf("deep.md missing")
	} else if f.Title != "Deep File" {
		t.Errorf("deep.md title = %q, want %q", f.Title, "Deep File")
	}
}

func TestScanMarkdown_Pattern(t *testing.T) {
	dir := t.TempDir()

	mustWriteContent(t, filepath.Join(dir, "readme.md"), "# Readme\n")
	mustWriteContent(t, filepath.Join(dir, "log-2023.md"), "# Log 2023\n")
	mustWriteContent(t, filepath.Join(dir, "log-2024.md"), "# Log 2024\n")
	mustWriteContent(t, filepath.Join(dir, "notes.txt"), "text file\n")

	files, err := ScanMarkdown(dir, "log-*.md")
	if err != nil {
		t.Fatalf("ScanMarkdown failed: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("ScanMarkdown returned %d files, want 2", len(files))
	}

	for _, f := range files {
		base := filepath.Base(f.Path)
		if base != "log-2023.md" && base != "log-2024.md" {
			t.Errorf("unexpected file in results: %s", base)
		}
	}
}

func TestScanMarkdown_Empty(t *testing.T) {
	dir := t.TempDir()

	mustWriteContent(t, filepath.Join(dir, "source.go"), "package main\n")

	files, err := ScanMarkdown(dir, "")
	if err != nil {
		t.Fatalf("ScanMarkdown failed: %v", err)
	}

	if len(files) != 0 {
		t.Fatalf("ScanMarkdown returned %d files, want 0", len(files))
	}
}

func mustWriteContent(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractMarkdownTitle(t *testing.T) {
	tests := []struct {
		name    string
		content string
		path    string
		want    string
	}{
		{
			name:    "first line header",
			content: "# My Document\nSome text.",
			path:    "doc.md",
			want:    "My Document",
		},
		{
			name:    "header with spaces",
			content: "  #   Spaced Header  \nText",
			path:    "space.md",
			want:    "Spaced Header",
		},
		{
			name:    "no header fallback to path",
			content: "Just some text\nwithout a header.",
			path:    "/path/to/my-file.md",
			want:    "my-file",
		},
		{
			name:    "header down a bit",
			content: "\n\n\n# Found It\n",
			path:    "x.md",
			want:    "Found It",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractMarkdownTitle(tc.content, tc.path)
			if got != tc.want {
				t.Errorf("extractMarkdownTitle(%q, %q) = %q; want %q", tc.content, tc.path, got, tc.want)
			}
		})
	}
}
