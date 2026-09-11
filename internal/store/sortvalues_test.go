package store

import (
	"testing"
)

func TestSortValuesFromDocumentColumns(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("notes", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}

	// UpsertDocument stamps created_at with wall-clock time at second
	// resolution inside the store, so ordering by created_at alone can tie.
	// Sort by line_count for the deterministic assertion and verify the
	// created_at/path/title keys are simply present.
	var ids []int64
	wantLines := map[int64]float64{}
	for i, lines := range []int{30, 10, 20} {
		path := "/tmp/note" + string(rune('a'+i)) + ".md"
		id, err := s.UpsertDocument(col.ID, path, "Note", "h", 1, lines)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		wantLines[id] = float64(lines)
	}

	values, err := s.SortValues(t.Context(), ids, "line_count")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != len(ids) {
		t.Fatalf("SortValues(line_count) returned %d entries, want %d", len(values), len(ids))
	}
	for id, want := range wantLines {
		if values[id] != want {
			t.Errorf("line_count[%d] = %v, want %v", id, values[id], want)
		}
	}

	// All document pseudo-fields resolve; regular fast fields fall through
	// to the fast_fields table (empty here).
	for _, field := range []string{"created_at", "mtime", "path", "title"} {
		got, err := s.SortValues(t.Context(), ids, field)
		if err != nil {
			t.Fatalf("SortValues(%s): %v", field, err)
		}
		if len(got) == 0 {
			t.Errorf("SortValues(%s) returned no values", field)
		}
	}
	if empty, err := s.SortValues(t.Context(), ids, "lang"); err != nil || len(empty) != 0 {
		t.Errorf("SortValues(fast field with no rows) = %v, %v; want empty, nil", empty, err)
	}
}

func TestSortValuesEmptyInput(t *testing.T) {
	s := newTestStore(t)
	values, err := s.SortValues(t.Context(), nil, "created_at")
	if err != nil || len(values) != 0 {
		t.Errorf("SortValues(nil) = %v, %v; want empty, nil", values, err)
	}
}
