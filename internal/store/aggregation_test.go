package store

import (
	"context"
	"strings"
	"testing"
)

func TestExecuteAggregationContext(t *testing.T) {
	s := newTestStore(t)
	markdown, err := s.CreateCollection("docs", CollectionTypeMarkdown, "/docs", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection markdown: %v", err)
	}
	code, err := s.CreateCollection("src", CollectionTypeCode, "/src", "**/*.go")
	if err != nil {
		t.Fatalf("CreateCollection code: %v", err)
	}

	documents := []struct {
		collection int64
		path       string
		lines      int
	}{
		{markdown.ID, "/docs/readme.md", 50},
		{markdown.ID, "/docs/guide.md", 150},
		{code.ID, "/src/main.go", 600},
		{code.ID, "/src/util.go", 50},
		{code.ID, "/src/test.go", 200},
	}
	var ids []int64
	for _, document := range documents {
		id, err := s.UpsertDocument(document.collection, document.path, document.path, "hash", 1, document.lines)
		if err != nil {
			t.Fatalf("UpsertDocument: %v", err)
		}
		ids = append(ids, id)
	}
	for _, id := range ids[:3] {
		if err := s.FastFields().Set(id, "lang", "go"); err != nil {
			t.Fatalf("set fast field: %v", err)
		}
	}
	if err := s.FastFields().Set(ids[0], "tags", "golang,concurrency"); err != nil {
		t.Fatalf("set tags fast field: %v", err)
	}
	if err := s.FastFields().Set(ids[1], "tags", "golang,web"); err != nil {
		t.Fatalf("set tags fast field: %v", err)
	}

	ctx := context.Background()
	tests := []struct {
		name string
		spec AggregationSpec
		want []AggregationBucket
	}{
		{name: "count", spec: AggregationSpec{Type: "count"}, want: []AggregationBucket{{Key: "count", Count: 5}}},
		{name: "terms", spec: AggregationSpec{Type: "terms", Field: "type"}, want: []AggregationBucket{{Key: "code", Count: 3}, {Key: "markdown", Count: 2}}},
		{name: "range", spec: AggregationSpec{Type: "range", Field: "line_count", Ranges: []string{"0-100", "100-500", "500-"}}, want: []AggregationBucket{{Key: "0-100", Count: 2}, {Key: "100-500", Count: 2}, {Key: "500-", Count: 1}}},
		{name: "fast field", spec: AggregationSpec{Type: "terms", Field: "lang"}, want: []AggregationBucket{{Key: "go", Count: 3}}},
		{name: "membership fast field terms", spec: AggregationSpec{Type: "terms", Field: "tags"}, want: []AggregationBucket{{Key: "golang", Count: 2}, {Key: "concurrency", Count: 1}, {Key: "web", Count: 1}}},
		{name: "mixed-case membership fast field terms", spec: AggregationSpec{Type: "terms", Field: "TAGS"}, want: []AggregationBucket{{Key: "golang", Count: 2}, {Key: "concurrency", Count: 1}, {Key: "web", Count: 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ExecuteAggregationContext(ctx, tt.spec, nil)
			if err != nil {
				t.Fatalf("ExecuteAggregationContext: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("buckets = %#v, want %#v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("bucket %d = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}

	filters := NewFilterSet()
	filters.Add(&CollectionFilter{Name: "src"})
	got, err := s.ExecuteAggregationContext(ctx, AggregationSpec{Type: "count"}, filters)
	if err != nil {
		t.Fatalf("filtered aggregation: %v", err)
	}
	if len(got) != 1 || got[0].Count != 3 {
		t.Fatalf("filtered count = %#v, want count/3", got)
	}
}

func TestAggregationRejectsUnsafeOrMalformedSpecs(t *testing.T) {
	unsafe := `type; DROP TABLE documents; --`
	if _, _, _, err := buildAggregationQuery(AggregationSpec{Type: "terms", Field: unsafe}); err == nil {
		t.Fatal("unsafe aggregation field was accepted")
	}
	if _, _, _, err := buildAggregationQuery(AggregationSpec{Type: "range", Field: "line_count", Ranges: []string{"broken"}}); err == nil {
		t.Fatal("malformed range was accepted")
	}
	if _, _, _, err := buildAggregationQuery(AggregationSpec{Type: "unknown"}); err == nil {
		t.Fatal("unknown aggregation type was accepted")
	}
}

func TestAggregationQueryUsesQuotedWhitelistedColumns(t *testing.T) {
	query, args, countOnly, err := buildAggregationQuery(AggregationSpec{Type: "histogram", Field: "created_at", Interval: "day"})
	if err != nil {
		t.Fatalf("buildAggregationQuery: %v", err)
	}
	if countOnly || len(args) != 1 || args[0] != "%Y-%m-%d" {
		t.Fatalf("histogram plan = query %q args %#v countOnly %v", query, args, countOnly)
	}
	if !strings.Contains(query, `strftime(?, "d"."created_at")`) {
		t.Fatalf("histogram query does not quote the whitelisted column: %q", query)
	}
}

func TestExecuteAggregationContextReturnsDatabaseErrors(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if _, err := s.ExecuteAggregationContext(context.Background(), AggregationSpec{Type: "count"}, nil); err == nil {
		t.Fatal("aggregation on closed store returned nil error")
	}
}

func TestDynamicFastFieldTermsAggregation(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("notes", CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	var ids []int64
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		id, err := s.UpsertDocument(col.ID, "/tmp/"+name, name, "h", 1, 1)
		if err != nil {
			t.Fatalf("UpsertDocument: %v", err)
		}
		ids = append(ids, id)
	}
	// A frontmatter-style key with no curated registry entry.
	for id, author := range []string{"jane", "jane", "bob"} {
		if err := s.FastFields().Set(ids[id], "author", author); err != nil {
			t.Fatalf("set author: %v", err)
		}
	}
	// Exact semantics: a comma-joined value stays ONE bucket (only curated
	// membership fields unnest tokens).
	if err := s.FastFields().Set(ids[0], "series", "a,b"); err != nil {
		t.Fatalf("set series: %v", err)
	}
	if err := s.UpsertFTS(ids[0], "a.md", "body"); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	got, err := s.ExecuteAggregationContext(ctx, AggregationSpec{Type: "terms", Field: "author"}, nil)
	if err != nil {
		t.Fatalf("dynamic terms: %v", err)
	}
	want := []AggregationBucket{{Key: "jane", Count: 2}, {Key: "bob", Count: 1}}
	if len(got) != len(want) {
		t.Fatalf("buckets = %#v, want %#v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("bucket %d = %#v, want %#v", i, got[i], want[i])
		}
	}

	got, err = s.ExecuteAggregationContext(ctx, AggregationSpec{Type: "terms", Field: "series"}, nil)
	if err != nil {
		t.Fatalf("dynamic exact terms: %v", err)
	}
	if len(got) != 1 || got[0].Key != "a,b" || got[0].Count != 1 {
		t.Errorf("series buckets = %#v, want one whole-value a,b bucket", got)
	}

	// Filters apply to dynamic fast-field terms like every other plan.
	filters := NewFilterSet()
	filters.Add(&FastFieldFilter{Field: "author", Value: "bob"})
	got, err = s.ExecuteAggregationContext(ctx, AggregationSpec{Type: "terms", Field: "author"}, filters)
	if err != nil {
		t.Fatalf("filtered dynamic terms: %v", err)
	}
	if len(got) != 1 || got[0].Key != "bob" || got[0].Count != 1 {
		t.Errorf("filtered buckets = %#v, want bob=1", got)
	}

	// Unknown names (neither indexed nor a whitelisted column) still error.
	if _, err := s.ExecuteAggregationContext(ctx, AggregationSpec{Type: "terms", Field: "nonexistent"}, nil); err == nil {
		t.Fatal("unknown aggregation field was accepted")
	} else if !strings.Contains(err.Error(), "unsupported aggregation field") {
		t.Errorf("error = %v, want unsupported aggregation field", err)
	}
}
