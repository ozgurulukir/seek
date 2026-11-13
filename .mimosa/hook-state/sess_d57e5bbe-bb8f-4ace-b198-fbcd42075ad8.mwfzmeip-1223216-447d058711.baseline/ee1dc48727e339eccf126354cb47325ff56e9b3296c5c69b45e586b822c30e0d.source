package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenRejectsQuestionMarkPath pins the M4 DSN contract: a '?' in the db
// path would be consumed as the SQLite parameter separator and silently
// corrupt the open, so it must be rejected with a clear error
// (review 2026-09-17 M4).
func TestOpenRejectsQuestionMarkPath(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "weird?name.db"))
	if err == nil {
		t.Fatal("expected error for db path containing '?', got nil")
	}
	if !strings.Contains(err.Error(), "'?'") {
		t.Fatalf("error should name the offending character, got %q", err)
	}
}
