package search

import (
	"reflect"
	"testing"
)

func TestParseAggregation_Comprehensive(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr bool
		want    Aggregation
	}{
		{
			name:    "terms aggregation",
			spec:    "type:terms",
			wantErr: false,
			want:    &TermAggregation{Field: "type"},
		},
		{
			name:    "histogram with default interval",
			spec:    "created_at:histogram",
			wantErr: false,
			want:    &HistogramAggregation{Field: "created_at", Interval: "month"},
		},
		{
			name:    "histogram with explicit interval",
			spec:    "created_at:histogram:day",
			wantErr: false,
			want:    &HistogramAggregation{Field: "created_at", Interval: "day"},
		},
		{
			name:    "range with default ranges",
			spec:    "line_count:range",
			wantErr: false,
			want:    &RangeAggregation{Field: "line_count", Ranges: []string{"0-100", "100-500", "500-"}},
		},
		{
			name:    "range with explicit ranges",
			spec:    "line_count:range:0-10,10-20",
			wantErr: false,
			want:    &RangeAggregation{Field: "line_count", Ranges: []string{"0-10", "10-20"}},
		},
		{
			name:    "count aggregation string",
			spec:    "count",
			wantErr: false,
			want:    &CountAggregation{},
		},
		{
			name:    "count aggregation format",
			spec:    "field:count",
			wantErr: false,
			want:    &CountAggregation{},
		},
		{
			name:    "invalid missing type",
			spec:    "field",
			wantErr: true,
			want:    nil,
		},
		{
			name:    "unknown type",
			spec:    "field:unknown",
			wantErr: true,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAggregation(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAggregation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseAggregation() = %v, want %v", got, tt.want)
			}
		})
	}
}
