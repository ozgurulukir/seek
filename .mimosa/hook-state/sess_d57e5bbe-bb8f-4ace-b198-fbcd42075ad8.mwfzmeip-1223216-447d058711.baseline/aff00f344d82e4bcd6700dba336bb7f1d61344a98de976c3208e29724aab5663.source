//go:build fts5 && sqlite_fts5

package store

import (
	"path/filepath"
	"testing"
)

// This test is deliberately fail-closed. Tagged CI/release builds promise
// FTS5, so a missing runtime capability must fail rather than skip the suite.
func TestFTS5RuntimeCapability(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "fts5-runtime.db"))
	if err != nil {
		t.Fatalf("tagged build does not provide a working FTS5 runtime: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}
