package cmd

import (
	"testing"
	"unicode/utf8"
)

func utf8Valid(s string) bool {
	return utf8.ValidString(s)
}

func TestFormatSnippet_UTF8SafeTruncation(t *testing.T) {
	// "İ" is a two-byte UTF-8 first rune. A byte-wise truncation at an odd
	// index would split it, producing an invalid UTF-8 replacement rune.
	input := "İstanbul İstanbul İstanbul İstanbul İstanbul"
	got := formatSnippet(input, 12)
	if len(got) == 0 {
		t.Fatal("empty snippet")
	}
	if got[len(got)-3:] != "..." {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
	if !utf8Valid(got) {
		t.Fatalf("snippet is not valid UTF-8: %q", got)
	}
}
