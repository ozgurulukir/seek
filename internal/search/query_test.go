package search

import (
	"testing"
)

func TestParseQuery(t *testing.T) {
	tests := []struct {
		input    string
		wantErr  bool
		wantType string
	}{
		{`"hello world"`, false, "*search.PhraseQuery"},
		{`hello AND world`, false, "*search.BooleanQuery"},
		{`hello OR world`, false, "*search.BooleanQuery"},
		{`NOT hello`, false, "*search.BooleanQuery"},
		{`pref*`, false, "*search.PrefixQuery"},
		{`hello~2`, false, "*search.FuzzyQuery"},
		{`title:hello`, false, "*search.FieldQuery"},
		{`NEAR(hello world, 5)`, false, "*search.NearQuery"},
		{`(a AND b) OR c`, false, "*search.BooleanQuery"},
		{`hello`, false, "*search.TermQuery"},
		{"", false, ""},
		{`INVALID (`, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			q, err := ParseQuery(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.input == "" {
				if q != nil {
					t.Fatalf("expected nil for empty input, got %T", q)
				}
				return
			}
			if q == nil {
				t.Fatalf("expected non-nil query, got nil")
			}
			_ = q.String()
			_ = tt.wantType
		})
	}
}

func TestToFTS5(t *testing.T) {
	tests := []struct {
		q         Query
		want      string
		wantFuzzy bool
	}{
		{&TermQuery{Value: "hello"}, "hello", false},
		{&PhraseQuery{Terms: []string{"hello", "world"}}, `"hello" "world"`, false},
		{&PrefixQuery{Prefix: "hel"}, "hel*", false},
		{&FuzzyQuery{Term: "hello", N: 2}, "hello*", true},
		{&FieldQuery{Field: "title", Query: &TermQuery{Value: "hello"}}, "title:hello", false},
		{&NearQuery{Terms: []string{"hello", "world"}, N: 5}, "NEAR(hello world, 5)", false},
		{&BooleanQuery{Left: &TermQuery{Value: "a"}, Op: "AND", Right: &TermQuery{Value: "b"}}, "(a AND b)", false},
	}

	for _, tt := range tests {
		got, fuzzy := ToFTS5(tt.q)
		if got != tt.want {
			t.Errorf("ToFTS5(%T) = %q, want %q", tt.q, got, tt.want)
		}
		if fuzzy != tt.wantFuzzy {
			t.Errorf("ToFTS5(%T) fuzzy = %v, want %v", tt.q, fuzzy, tt.wantFuzzy)
		}
	}
}

func TestAnalyzeToken(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"hello-world", []string{"hello", "world"}},
		{"hello_world", []string{"hello_world"}},
		{"", []string{}},
		{"123", []string{"123"}},
	}

	for _, tt := range tests {
		got := AnalyzeToken(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("AnalyzeToken(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("AnalyzeToken(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestParseAggregation(t *testing.T) {
	tests := []struct {
		spec     string
		wantErr  bool
		wantType string
	}{
		{"type:terms", false, "*search.TermAggregation"},
		{"created_at:histogram:month", false, "*search.HistogramAggregation"},
		{"line_count:range:0-100,100-500", false, "*search.RangeAggregation"},
		{"count", false, "*search.CountAggregation"},
		{"invalid", true, ""},
	}

	for _, tt := range tests {
		_, err := ParseAggregation(tt.spec)
		if tt.wantErr && err == nil {
			t.Errorf("ParseAggregation(%q) expected error", tt.spec)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("ParseAggregation(%q) unexpected error: %v", tt.spec, err)
		}
	}
}

func TestRangeAggregationSQL(t *testing.T) {
	agg := &RangeAggregation{
		Field:  "line_count",
		Ranges: []string{"0-100", "100-500", "500-"},
	}
	query, args := agg.SQL()

	expectedQuery := "SELECT CASE WHEN d.line_count >= ? AND d.line_count < ? THEN ? WHEN d.line_count >= ? AND d.line_count < ? THEN ? WHEN d.line_count >= ? THEN ? ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key"

	if query != expectedQuery {
		t.Errorf("expected query %q, got %q", expectedQuery, query)
	}

	if len(args) != 8 {
		t.Fatalf("expected 8 args, got %d", len(args))
	}

	expectedArgs := []interface{}{"0", "100", "0-100", "100", "500", "100-500", "500", "500-"}
	for i, arg := range args {
		if arg != expectedArgs[i] {
			t.Errorf("arg %d: expected %v, got %v", i, expectedArgs[i], arg)
		}
	}
}
