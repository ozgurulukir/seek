package store

import (
	"strings"
	"testing"
)

func TestExecIgnoreDuplicateReturnsUnexpectedError(t *testing.T) {
	s := newTestStore(t)
	err := s.execIgnoreDuplicate(`ALTER TABLE missing_table ADD COLUMN value TEXT`)
	if err == nil {
		t.Fatal("expected migration error")
	}
	if !strings.Contains(err.Error(), "missing_table") {
		t.Fatalf("error = %q, want statement context", err)
	}
}

func TestExecIgnoreDuplicateAcceptsExistingColumn(t *testing.T) {
	s := newTestStore(t)
	if err := s.execIgnoreDuplicate(`ALTER TABLE chunks ADD COLUMN start_line INTEGER DEFAULT 0`); err != nil {
		t.Fatalf("duplicate column should be ignored: %v", err)
	}
}
