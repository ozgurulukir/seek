package search

import (
	"strings"
	"testing"
)

func TestTermAggregation_SQL(t *testing.T) {
	tests := []struct {
		name          string
		field         string
		expectedField string
	}{
		{"type", "type", "c.type"},
		{"doc_type", "doc_type", "c.type"},
		{"collection", "collection", "c.name"},
		{"created_at", "created_at", "d.created_at"},
		{"date", "date", "d.created_at"},
		{"line_count", "line_count", "d.line_count"},
		{"path", "path", "d.path"},
		{"unknown field", "unknown", "c.unknown"},
		{"field with dot", "c.custom", "c.custom"},
		{"field with dot (d)", "d.custom", "d.custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &TermAggregation{Field: tt.field}
			sql, args := agg.SQL()
			if len(args) != 0 {
				t.Errorf("Expected 0 args, got %d", len(args))
			}

			// We check if the expected field name was mapped correctly
			if !strings.Contains(sql, tt.expectedField+" as key") {
				t.Errorf("Expected SQL to contain %q, but got: %s", tt.expectedField+" as key", sql)
			}
			if !strings.Contains(sql, "GROUP BY "+tt.expectedField) {
				t.Errorf("Expected SQL to contain GROUP BY %s, but got: %s", tt.expectedField, sql)
			}
		})
	}
}

func TestHistogramAggregation_SQL(t *testing.T) {
	tests := []struct {
		name           string
		interval       string
		expectedFormat string
	}{
		{"day", "day", "%Y-%m-%d"},
		{"week", "week", "%Y-%W"},
		{"month", "month", "%Y-%m"},
		{"year", "year", "%Y"},
		{"default/unknown", "unknown", "%Y-%m"},
		{"empty", "", "%Y-%m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &HistogramAggregation{Field: "created_at", Interval: tt.interval}
			sql, args := agg.SQL()
			if len(args) != 0 {
				t.Errorf("Expected 0 args, got %d", len(args))
			}

			expectedStrftime := "strftime('" + tt.expectedFormat + "', d.created_at) as key"
			if !strings.Contains(sql, expectedStrftime) {
				t.Errorf("Expected SQL to contain %q, but got: %s", expectedStrftime, sql)
			}
		})
	}
}

func TestRangeAggregation_SQL(t *testing.T) {
	tests := []struct {
		name          string
		ranges        []string
		expectedCases []string
	}{
		{
			name:   "standard ranges",
			ranges: []string{"0-100", "100-500", "500-"},
			expectedCases: []string{
				"WHEN d.line_count >= 0 AND d.line_count < 100 THEN '0-100'",
				"WHEN d.line_count >= 100 AND d.line_count < 500 THEN '100-500'",
				"WHEN d.line_count >= 500 THEN '500-'",
			},
		},
		{
			name:   "invalid ranges ignored",
			ranges: []string{"invalid", "0-100"},
			expectedCases: []string{
				"WHEN d.line_count >= 0 AND d.line_count < 100 THEN '0-100'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &RangeAggregation{Field: "line_count", Ranges: tt.ranges}
			sql, args := agg.SQL()
			if len(args) != 0 {
				t.Errorf("Expected 0 args, got %d", len(args))
			}

			for _, expectedCase := range tt.expectedCases {
				if !strings.Contains(sql, expectedCase) {
					t.Errorf("Expected SQL to contain %q, but got: %s", expectedCase, sql)
				}
			}
		})
	}
}

func TestCountAggregation_SQL(t *testing.T) {
	agg := &CountAggregation{}
	sql, args := agg.SQL()

	if len(args) != 0 {
		t.Errorf("Expected 0 args, got %d", len(args))
	}

	expectedSQL := "SELECT COUNT(*) FROM documents d JOIN collections c ON c.id = d.collection_id"
	if sql != expectedSQL {
		t.Errorf("Expected SQL %q, but got: %q", expectedSQL, sql)
	}
}
