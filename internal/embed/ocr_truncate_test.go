package embed

import (
	"testing"
	"unicode/utf8"
)

func TestTruncate_UTF8Safe(t *testing.T) {
	for _, tc := range []struct{ in string; n int }{
		{"İstanbul", 3},
		{"hello", 10},
		{"", 0},
		{"abc", -1},
	} {
		if got := truncate(tc.in, tc.n); !utf8.ValidString(got) {
			t.Fatalf("truncate(%q,%d)=%q is not valid UTF-8", tc.in, tc.n, got)
		}
	}
	// A clamp value below 1 must not panic or produce empty/invalid output.
	if got := truncate("abc", -5); !utf8.ValidString(got) {
		t.Fatalf("negative n should clamp safely, got %q", got)
	}
}
