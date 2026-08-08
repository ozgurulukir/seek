package store

import (
	"testing"
)

func TestFilterSetEmpty(t *testing.T) {
	fs := NewFilterSet()
	clause, args := fs.ToSQL()
	if clause != "" {
		t.Errorf("expected empty clause, got: %s", clause)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got: %v", args)
	}
}

func TestCollectionFilter(t *testing.T) {
	f := &CollectionFilter{Name: "notes"}
	clause, args := f.ToSQL()
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 1 || args[0] != "notes" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestDocTypeFilter(t *testing.T) {
	f := &DocTypeFilter{Type: "markdown"}
	clause, args := f.ToSQL()
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 1 || args[0] != "markdown" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestDateRangeFilter(t *testing.T) {
	f := &DateRangeFilter{After: "2024-01-01T00:00:00Z", Before: "2024-12-31T23:59:59Z"}
	clause, args := f.ToSQL()
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}

func TestDateRangeFilterAfterOnly(t *testing.T) {
	f := &DateRangeFilter{After: "2024-01-01T00:00:00Z"}
	clause, args := f.ToSQL()
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %d", len(args))
	}
}

func TestChunkTypeFilter(t *testing.T) {
	f := &ChunkTypeFilter{Type: 1}
	clause, args := f.ToSQL()
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 1 || args[0] != 1 {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestPathFilter(t *testing.T) {
	f := &PathFilter{Pattern: "docs/*"}
	clause, args := f.ToSQL()
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %d", len(args))
	}
}

func TestPathFilterRejectsTraversal(t *testing.T) {
	f := &PathFilter{Pattern: "../etc/passwd"}
	clause, args := f.ToSQL()
	if clause != "" {
		t.Errorf("expected empty clause for traversal, got: %s", clause)
	}
	if len(args) != 0 {
		t.Errorf("expected no args for traversal, got: %v", args)
	}
}

func TestFilterSetComposition(t *testing.T) {
	fs := NewFilterSet()
	fs.Add(&CollectionFilter{Name: "notes"})
	fs.Add(&DocTypeFilter{Type: "markdown"})
	clause, args := fs.ToSQL()
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}
