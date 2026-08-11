package search

import (
	"fmt"
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

func TestTermAggregationSQLInjection(t *testing.T) {
	// A malicious payload mimicking an injection attempt
	maliciousField := "type; DROP TABLE documents; --"
	agg := &TermAggregation{Field: maliciousField}
	query, _ := agg.SQL()

	// Ensure the generated SQL safely quotes the entire identifier (e.g. "c"."type; DROP TABLE documents; --")
	// so that it cannot break out of the SELECT and GROUP BY clauses.
	expectedQueryStr := `SELECT "c"."type; DROP TABLE documents; --" as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY "c"."type; DROP TABLE documents; --" ORDER BY count DESC`

	if query != expectedQueryStr {
		t.Errorf("Expected SQL query to escape malicious field.\nGot: %s\nWant: %s", query, expectedQueryStr)
	}

	// Another test to check if embedded quotes are escaped properly
	maliciousField2 := `type"; DROP TABLE documents; --`
	agg2 := &TermAggregation{Field: maliciousField2}
	query2, _ := agg2.SQL()
	expectedQueryStr2 := `SELECT "c"."type""; DROP TABLE documents; --" as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY "c"."type""; DROP TABLE documents; --" ORDER BY count DESC`

	if query2 != expectedQueryStr2 {
		t.Errorf("Expected SQL query to escape double quotes.\nGot: %s\nWant: %s", query2, expectedQueryStr2)
	}
}

func TestTermAggregationSQL(t *testing.T) {
	// Test standard mapped fields that should pass through normally
	cases := []struct {
		input    string
		expected string
	}{
		{"type", `"c"."type"`},
		{"created_at", `"d"."created_at"`},
		{"line_count", `"d"."line_count"`},
		{"path", `"d"."path"`},
		{"collection", `"c"."name"`},
		{"some_other", `"c"."some_other"`},
	}

	for _, tc := range cases {
		agg := &TermAggregation{Field: tc.input}
		query, _ := agg.SQL()
		expectedSQL := fmt.Sprintf(`SELECT %s as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY %s ORDER BY count DESC`, tc.expected, tc.expected)
		if query != expectedSQL {
			t.Errorf("Expected SQL query for %q to be:\n%s\nGot:\n%s", tc.input, expectedSQL, query)
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
		t.Fatalf("expected 8 args, got %d: %v", len(args), args)
	}

	expectedArgs := []interface{}{"0", "100", "0-100", "100", "500", "100-500", "500", "500-"}
	for i, arg := range args {
		if arg != expectedArgs[i] {
			t.Errorf("arg %d: expected %v, got %v", i, expectedArgs[i], arg)
		}
	}
}

func TestLowerToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"HELLO", "hello"},
		{"world", "world"},
		{"MiXeD", "mixed"},
		{"123", "123"},
		{"", ""},
		{"ÄÖÜ", "äöü"},
		{"Γεια", "γεια"},
		{"Привет", "привет"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := LowerToken(tt.input); got != tt.want {
				t.Errorf("LowerToken(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToFTS5WithAnalyzer(t *testing.T) {
	a := NewAnalyzer("en", true, true)
	tests := []struct {
		q         Query
		want      string
		wantFuzzy bool
	}{
		{nil, "", false},
		{&TermQuery{Value: "running"}, "run*", false},
		{&TermQuery{Value: "the"}, "the", false},
		{&PhraseQuery{Terms: []string{"running", "jumps"}}, `"run*" "jump*"`, false},
		{&PhraseQuery{Terms: []string{"the", "running"}}, `"the" "run*"`, false},
		{&PrefixQuery{Prefix: "running"}, "run*", false},
		{&PrefixQuery{Prefix: "the"}, "the*", false},
		{&FuzzyQuery{Term: "running", N: 2}, "run*", true},
		{&FuzzyQuery{Term: "the", N: 2}, "the*", true},
		{&FieldQuery{Field: "title", Query: &TermQuery{Value: "running"}}, "title:run*", false},
		{&NearQuery{Terms: []string{"running", "jumps"}, N: 5}, "NEAR(run* jump*, 5)", false},
		{&BooleanQuery{Left: &TermQuery{Value: "running"}, Op: "AND", Right: &TermQuery{Value: "jumps"}}, "(run* AND jump*)", false},
		{&BooleanQuery{Left: &TermQuery{Value: "running"}, Op: "NOT", Right: &TermQuery{Value: "jumps"}}, "(run* NOT jump*)", false},
	}

	for _, tt := range tests {
		got, fuzzy := ToFTS5WithAnalyzer(tt.q, a)
		if got != tt.want {
			t.Errorf("ToFTS5WithAnalyzer(%T) = %q, want %q", tt.q, got, tt.want)
		}
		if fuzzy != tt.wantFuzzy {
			t.Errorf("ToFTS5WithAnalyzer(%T) fuzzy = %v, want %v", tt.q, fuzzy, tt.wantFuzzy)
		}
	}
}
