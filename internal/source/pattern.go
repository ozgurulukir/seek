package source

import (
	"fmt"
	"path/filepath"
	"strings"
)

// checkScanPattern rejects collection patterns the scanners cannot honor.
// Pattern matching is basename-only (filepath.Match has no cross-directory
// `**` semantics), so a pattern containing a path separator — `docs/**/*.md`
// — can never match and would silently index nothing. Failing fast at scan
// entry names the problem and the fix instead (review 2026-09-17 M8). The
// hard-coded defaults are exempt: the scanners special-case them and skip
// matching entirely.
func checkScanPattern(pattern string, defaults ...string) error {
	for _, d := range defaults {
		if pattern == d {
			return nil
		}
	}
	if strings.ContainsAny(pattern, `/\`) {
		return fmt.Errorf("pattern %q contains a path separator: -p/--pattern matches file names only (e.g. \"*.md\", \"notes-*\"); point the collection at the subdirectory instead of globbing into it", pattern)
	}
	// Reject malformed globs (e.g. an unterminated '[') up front so the walk
	// never sees them. filepath.Match reports ErrBadPattern for any name.
	if _, err := filepath.Match(pattern, "probe"); err != nil {
		return fmt.Errorf("pattern %q: %w", pattern, err)
	}
	return nil
}
