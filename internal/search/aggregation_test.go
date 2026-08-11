package search

import (
	"strings"
	"testing"
)

func TestHistogramAggregation_SQL(t *testing.T) {
	tests := []struct {
		name           string
		interval       string
		expectedFormat string
	}{
		{
			name:           "day interval",
			interval:       "day",
			expectedFormat: "%Y-%m-%d",
		},
		{
			name:           "week interval",
			interval:       "week",
			expectedFormat: "%Y-%W",
		},
		{
			name:           "month interval",
			interval:       "month",
			expectedFormat: "%Y-%m",
		},
		{
			name:           "year interval",
			interval:       "year",
			expectedFormat: "%Y",
		},
		{
			name:           "default interval (empty)",
			interval:       "",
			expectedFormat: "%Y-%m",
		},
		{
			name:           "unknown interval",
			interval:       "unknown",
			expectedFormat: "%Y-%m",
		},
		{
			name:           "case insensitive interval",
			interval:       "DAY",
			expectedFormat: "%Y-%m-%d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &HistogramAggregation{
				Field:    "created_at",
				Interval: tt.interval,
			}
			query, args := agg.SQL()

			if len(args) != 0 {
				t.Errorf("HistogramAggregation.SQL() expected 0 args, got %d", len(args))
			}

			expectedFragment := "strftime('" + tt.expectedFormat + "', d.created_at)"
			if !strings.Contains(query, expectedFragment) {
				t.Errorf("HistogramAggregation.SQL() query = %v, expected it to contain %v", query, expectedFragment)
			}
		})
	}
}
