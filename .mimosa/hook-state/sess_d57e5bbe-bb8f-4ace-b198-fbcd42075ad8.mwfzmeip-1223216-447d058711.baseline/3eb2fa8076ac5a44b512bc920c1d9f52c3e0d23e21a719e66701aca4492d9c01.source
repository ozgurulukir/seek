package store

import (
	"strings"
	"testing"
)

// mustToSQL invokes ToSQL and fails the test on error, so happy-path tests
// only see clause/args.
func mustToSQL(t *testing.T, f Filter) (string, []interface{}) {
	t.Helper()
	clause, args, err := f.ToSQL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return clause, args
}

func TestFilterSetEmpty(t *testing.T) {
	fs := NewFilterSet()
	clause, args := mustToSQL(t, fs)
	if clause != "" {
		t.Errorf("expected empty clause, got: %s", clause)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got: %v", args)
	}
}

func TestCollectionFilter(t *testing.T) {
	f := &CollectionFilter{Name: "notes"}
	clause, args := mustToSQL(t, f)
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 1 || args[0] != "notes" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestDocTypeFilter(t *testing.T) {
	f := &DocTypeFilter{Type: "markdown"}
	clause, args := mustToSQL(t, f)
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 1 || args[0] != "markdown" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestDateRangeFilter(t *testing.T) {
	f := &DateRangeFilter{After: "2024-01-01T00:00:00Z", Before: "2024-12-31T23:59:59Z"}
	clause, args := mustToSQL(t, f)
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}

func TestDateRangeFilterAfterOnly(t *testing.T) {
	f := &DateRangeFilter{After: "2024-01-01T00:00:00Z"}
	clause, args := mustToSQL(t, f)
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %d", len(args))
	}
}

func TestChunkTypeFilter(t *testing.T) {
	f := &ChunkTypeFilter{Type: 1}
	clause, args := mustToSQL(t, f)
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 1 || args[0] != 1 {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestPathFilter(t *testing.T) {
	f := &PathFilter{Pattern: "docs/*"}
	clause, args := mustToSQL(t, f)
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %d", len(args))
	}
}

func TestPathFilterRejectsTraversal(t *testing.T) {
	f := &PathFilter{Pattern: "../etc/passwd"}
	clause, args, err := f.ToSQL()
	if err == nil {
		t.Fatal("expected error for traversal pattern, got nil")
	}
	if !strings.Contains(err.Error(), "..") {
		t.Errorf("error should mention traversal, got: %v", err)
	}
	if clause != "" || args != nil {
		t.Errorf("expected empty clause/args on error, got: %q %v", clause, args)
	}
}

func TestFilterSetRejectsTraversal(t *testing.T) {
	fs := NewFilterSet()
	fs.Add(&CollectionFilter{Name: "notes"})
	fs.Add(&PathFilter{Pattern: "../etc/passwd"})
	clause, _, err := fs.ToSQL()
	if err == nil {
		t.Fatal("expected FilterSet.ToSQL to propagate path filter error, got nil")
	}
	if clause != "" {
		t.Errorf("expected empty clause on error, got: %q", clause)
	}
}

func TestFilterSetComposition(t *testing.T) {
	fs := NewFilterSet()
	fs.Add(&CollectionFilter{Name: "notes"})
	fs.Add(&DocTypeFilter{Type: "markdown"})
	clause, args := mustToSQL(t, fs)
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}

func TestTagFilter(t *testing.T) {
	f := &TagFilter{Tag: "go"}
	clause, args := mustToSQL(t, f)
	if clause == "" {
		t.Error("expected non-empty clause")
	}
	if len(args) != 4 || args[0] != "go" || args[1] != "go" || args[2] != "go" || args[3] != "go" {
		t.Errorf("unexpected args: %v", args)
	}
	if !strings.Contains(clause, "field_name = 'tags'") {
		t.Errorf("clause should target tags field: %s", clause)
	}
}

func TestTagFilter_MatchesCommaSeparatedValues(t *testing.T) {
	// End-to-end: a doc tagged "go,rust" must match searches for "go",
	// "rust", but not "g" or "oc".
	s := newTestStore(t)
	col, err := s.CreateCollection("notes", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := s.UpsertDocument(col.ID, "/tmp/note.md", "Note", "h", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FastFields().Set(docID, "tags", "go,rust"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFTS(docID, "Note", "note body content"); err != nil {
		t.Fatal(err)
	}

	match := func(tag string) bool {
		fs := NewFilterSet()
		fs.Add(&TagFilter{Tag: tag})
		results, err := s.SearchFTS("Note", 10, fs)
		if err != nil {
			t.Fatal(err)
		}
		return len(results) > 0
	}
	if !match("go") {
		t.Error("tag 'go' should match 'go,rust'")
	}
	if !match("rust") {
		t.Error("tag 'rust' should match 'go,rust'")
	}
	if match("g") {
		t.Error("tag 'g' should not match 'go,rust' (prefix leak)")
	}
	if match("oc") {
		t.Error("tag 'oc' should not match 'go,rust' (substring leak)")
	}
}

func TestValidFastField(t *testing.T) {
	cases := []struct {
		field string
		want  bool
	}{
		{"lang", true}, {"repo", true}, {"tags", true},
		{"topics", true}, {"entities", true}, {"language", true},
		{"workspace", true}, {"filename", true}, {"rel_path", true}, {"ext", true},
		// parserdef conversation-context fields.
		{"parent", true}, {"platform", true}, {"profile", true}, {"channel", true}, {"model", true},
		{"", false}, {"title", false}, {"created_at", false}, // document pseudo-fields are sort-only
		{"content", false}, {"name;DROP", false},
		{"language ", false}, // whitespace is not trimmed / whitelisted
	}
	for _, c := range cases {
		if got := ValidFastField(c.field); got != c.want {
			t.Errorf("ValidFastField(%q) = %v, want %v", c.field, got, c.want)
		}
	}
}

func TestFastFieldFilterToSQL(t *testing.T) {
	// topics is a multi-value (membership) field: SQL uses token matching.
	f := &FastFieldFilter{Field: "topics", Value: "concurrency"}
	mode, ok := fastFieldMatchMode("topics")
	if !ok || mode != FastFieldMembership {
		t.Fatalf("topics should be membership field, got mode=%v ok=%v", mode, ok)
	}
	sql, args, err := f.ToSQL()
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if len(args) != 5 || args[0] != "topics" {
		t.Fatalf("membership ToSQL args = %v, want [topics, value*4]", args)
	}
	if !strings.Contains(sql, "LIKE") {
		t.Fatalf("membership SQL should use LIKE token matching, got: %s", sql)
	}

	// language is a single-value (exact) field: exact equality.
	fe := &FastFieldFilter{Field: "language", Value: "en"}
	me, ok := fastFieldMatchMode("language")
	if !ok || me != FastFieldExact {
		t.Fatalf("language should be exact field, got mode=%v", me)
	}
	sqlE, argsE, err := fe.ToSQL()
	if err != nil {
		t.Fatalf("ToSQL exact: %v", err)
	}
	if len(argsE) != 2 || argsE[0] != "language" {
		t.Fatalf("exact ToSQL args = %v, want [language <encoded>]", argsE)
	}
	if !strings.Contains(sqlE, "field_value = ?") {
		t.Fatalf("exact SQL should use equality, got: %s", sqlE)
	}
}
