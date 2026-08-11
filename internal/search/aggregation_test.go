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
		{"custom_no_dot", "custom", "c.custom"},
		{"custom_with_dot", "c.custom", "c.custom"},
		{"other_with_dot", "d.other", "d.other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &TermAggregation{Field: tt.field}
			query, args := agg.SQL()
			if len(args) != 0 {
				t.Errorf("Expected 0 args, got %d", len(args))
			}

			if !strings.Contains(query, tt.expectedField+" as key") || !strings.Contains(query, "GROUP BY "+tt.expectedField) {
				t.Errorf("Expected field %q in query, got: %s", tt.expectedField, query)
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
		{"default", "unknown", "%Y-%m"},
		{"empty", "", "%Y-%m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &HistogramAggregation{Field: "created_at", Interval: tt.interval}
			query, args := agg.SQL()
			if len(args) != 0 {
				t.Errorf("Expected 0 args, got %d", len(args))
			}

			expectedStrftime := "strftime('" + tt.expectedFormat + "', d.created_at)"
			if !strings.Contains(query, expectedStrftime) {
				t.Errorf("Expected %q in query, got: %s", expectedStrftime, query)
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
			name:   "single_closed",
			ranges: []string{"0-100"},
			expectedCases: []string{
				"WHEN d.line_count >= 0 AND d.line_count < 100 THEN '0-100'",
			},
		},
		{
			name:   "single_open",
			ranges: []string{"500-"},
			expectedCases: []string{
				"WHEN d.line_count >= 500 THEN '500-'",
			},
		},
		{
			name:   "multiple",
			ranges: []string{"0-100", "100-500", "500-"},
			expectedCases: []string{
				"WHEN d.line_count >= 0 AND d.line_count < 100 THEN '0-100'",
				"WHEN d.line_count >= 100 AND d.line_count < 500 THEN '100-500'",
				"WHEN d.line_count >= 500 THEN '500-'",
			},
		},
		{
			name:   "invalid_format",
			ranges: []string{"invalid"},
			expectedCases: []string{}, // No case statements generated
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &RangeAggregation{Field: "line_count", Ranges: tt.ranges}
			query, args := agg.SQL()
			if len(args) != 0 {
				t.Errorf("Expected 0 args, got %d", len(args))
			}

			if len(tt.expectedCases) == 0 {
				if strings.Contains(query, "WHEN") {
					t.Errorf("Expected no WHEN clauses, got: %s", query)
				}
			} else {
				for _, expectedCase := range tt.expectedCases {
					if !strings.Contains(query, expectedCase) {
						t.Errorf("Expected %q in query, got: %s", expectedCase, query)
					}
				}
			}
		})
	}
}

func TestCountAggregation_SQL(t *testing.T) {
	agg := &CountAggregation{}
	query, args := agg.SQL()

	if len(args) != 0 {
		t.Errorf("Expected 0 args, got %d", len(args))
	}

	expectedQuery := "SELECT COUNT(*) FROM documents d JOIN collections c ON c.id = d.collection_id"
	if query != expectedQuery {
		t.Errorf("Expected query %q, got: %s", expectedQuery, query)
	}
}

func TestParseAggregation_Aggs(t *testing.T) {
	tests := []struct {
		name      string
		spec      string
		wantType  interface{}
		wantErr   bool
	}{
		{"count", "count", &CountAggregation{}, false},
		{"terms", "type:terms", &TermAggregation{}, false},
		{"histogram_default", "created_at:histogram", &HistogramAggregation{}, false},
		{"histogram_with_interval", "created_at:histogram:year", &HistogramAggregation{}, false},
		{"range_default", "line_count:range", &RangeAggregation{}, false},
		{"range_custom", "line_count:range:0-10,10-", &RangeAggregation{}, false},

		{"invalid_spec", "invalid", nil, true},
		{"unknown_type", "field:unknown", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAggregation(tt.spec)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAggregation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				switch want := tt.wantType.(type) {
				case *CountAggregation:
					if _, ok := got.(*CountAggregation); !ok {
						t.Errorf("ParseAggregation() got %T, want %T", got, want)
					}
				case *TermAggregation:
					if g, ok := got.(*TermAggregation); !ok {
						t.Errorf("ParseAggregation() got %T, want %T", got, want)
					} else {
						// Extract expected field from spec
						expectedField := strings.SplitN(tt.spec, ":", 2)[0]
						if g.Field != expectedField {
							t.Errorf("Expected field %q, got %q", expectedField, g.Field)
						}
					}
				case *HistogramAggregation:
					if g, ok := got.(*HistogramAggregation); !ok {
						t.Errorf("ParseAggregation() got %T, want %T", got, want)
					} else {
						parts := strings.SplitN(tt.spec, ":", 3)
						expectedField := parts[0]
						expectedInterval := "month" // default
						if len(parts) == 3 {
							expectedInterval = parts[2]
						}

						if g.Field != expectedField {
							t.Errorf("Expected field %q, got %q", expectedField, g.Field)
						}
						if g.Interval != expectedInterval {
							t.Errorf("Expected interval %q, got %q", expectedInterval, g.Interval)
						}
					}
				case *RangeAggregation:
					if g, ok := got.(*RangeAggregation); !ok {
						t.Errorf("ParseAggregation() got %T, want %T", got, want)
					} else {
						parts := strings.SplitN(tt.spec, ":", 3)
						expectedField := parts[0]
						var expectedRanges []string
						if len(parts) == 3 && parts[2] != "" {
							expectedRanges = strings.Split(parts[2], ",")
						} else {
							expectedRanges = []string{"0-100", "100-500", "500-"} // default
						}

						if g.Field != expectedField {
							t.Errorf("Expected field %q, got %q", expectedField, g.Field)
						}

						if len(g.Ranges) != len(expectedRanges) {
							t.Errorf("Expected %d ranges, got %d", len(expectedRanges), len(g.Ranges))
						} else {
							for i, r := range expectedRanges {
								if g.Ranges[i] != r {
									t.Errorf("Expected range at index %d to be %q, got %q", i, r, g.Ranges[i])
								}
							}
						}
					}
				}
			}
		})
	}
}
