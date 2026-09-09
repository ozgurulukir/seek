package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]string
	}{
		{
			name:    "basic scalars",
			content: "---\ntitle: My Note\ntags: go,rust\n---\n\n# Body\n",
			want:    map[string]string{"title": "My Note", "tags": "go,rust"},
		},
		{
			name:    "list items join with comma",
			content: "---\ntags:\n  - go\n  - rust\nlang: go\n---\nbody",
			want:    map[string]string{"tags": "go,rust", "lang": "go"},
		},
		{
			name:    "quoted values",
			content: "---\nauthor: \"Jane Doe\"\n---\n",
			want:    map[string]string{"author": "Jane Doe"},
		},
		{
			name:    "keys lowercased",
			content: "---\nTags: Go\n---\n",
			want:    map[string]string{"tags": "Go"},
		},
		{
			name:    "no frontmatter",
			content: "# Just a heading\n",
			want:    map[string]string{},
		},
		{
			name:    "list after second scalar attaches correctly",
			content: "---\nlang: go\ntags:\n  - a\n  - b\n---\n",
			want:    map[string]string{"lang": "go", "tags": "a,b"},
		},
		{
			name:    "empty values skipped",
			content: "---\ntitle:\nauthor: Jane\n---\n",
			want:    map[string]string{"author": "Jane"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFrontmatter(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %q = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestScanMarkdown_ExtractsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("---\ntags: go,testing\ndate: 2026-01-15\n---\n\n# Note\n\nbody text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, issues, err := ScanMarkdown(dir, "**/*.md")
	if err != nil {
		t.Fatalf("ScanMarkdown: %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	f := files[0]
	if f.Metadata["tags"] != "go,testing" {
		t.Errorf("tags = %q, want go,testing", f.Metadata["tags"])
	}
	if f.Metadata["date"] != "2026-01-15" {
		t.Errorf("date = %q, want 2026-01-15", f.Metadata["date"])
	}
}
