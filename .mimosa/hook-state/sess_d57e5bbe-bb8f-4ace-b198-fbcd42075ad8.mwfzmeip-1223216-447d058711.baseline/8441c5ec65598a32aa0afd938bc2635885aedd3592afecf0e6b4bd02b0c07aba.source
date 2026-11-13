package source

import (
	"strings"
	"testing"
)

// TestCheckScanPattern pins the M8 contract: patterns the basename matcher
// cannot honor (separator globs, malformed globs) fail fast with an explicit
// error instead of silently indexing nothing; basename patterns and the
// hard-coded defaults stay accepted (review 2026-09-17 M8).
func TestCheckScanPattern(t *testing.T) {
	codeDefaults := []string{"**/*", "*"}
	markdownDefaults := []string{"**/*.md", "**/*.markdown"}

	accept := func(name string, err error) {
		t.Helper()
		if err != nil {
			t.Errorf("%s: expected accept, got %v", name, err)
		}
	}
	reject := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: expected reject, got nil", name)
			return
		}
		if !strings.Contains(err.Error(), "separator") && !strings.Contains(err.Error(), "pattern") {
			t.Errorf("%s: error should name the pattern problem, got %q", name, err)
		}
	}

	accept("empty code pattern", checkScanPattern("", codeDefaults...))
	accept("basename glob", checkScanPattern("notes-*.md", markdownDefaults...))
	accept("markdown default", checkScanPattern("**/*.md", markdownDefaults...))
	accept("code default", checkScanPattern("**/*", codeDefaults...))

	reject("directory glob", checkScanPattern("docs/**/*.md", markdownDefaults...))
	reject("relative path glob", checkScanPattern("a/b*.go", codeDefaults...))
	reject("malformed glob", checkScanPattern("[bad", codeDefaults...))
}

// TestScanMarkdownRejectsSeparatorPattern verifies the end-to-end scan entry
// fails loudly for a separator pattern instead of returning an empty file
// list with no explanation.
func TestScanMarkdownRejectsSeparatorPattern(t *testing.T) {
	_, _, err := ScanMarkdown(t.TempDir(), "docs/**/*.md")
	if err == nil {
		t.Fatal("expected error for separator-containing pattern, got nil")
	}
	if !strings.Contains(err.Error(), "separator") {
		t.Fatalf("error should explain the separator problem, got %q", err)
	}
}

// TestScanCodeRejectsSeparatorPattern is the code-scanner counterpart.
func TestScanCodeRejectsSeparatorPattern(t *testing.T) {
	_, err := ScanCode(t.TempDir(), "internal/**/*.go")
	if err == nil {
		t.Fatal("expected error for separator-containing pattern, got nil")
	}
	if !strings.Contains(err.Error(), "separator") {
		t.Fatalf("error should explain the separator problem, got %q", err)
	}
}
