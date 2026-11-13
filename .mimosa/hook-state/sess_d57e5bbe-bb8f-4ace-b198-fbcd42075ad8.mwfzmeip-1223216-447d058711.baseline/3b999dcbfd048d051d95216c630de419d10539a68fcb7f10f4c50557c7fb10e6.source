package search

import (
	"testing"
)

func TestParseAggregationSpecs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want AggregationSpec
	}{
		{
			name: "count simple",
			text: "count",
			want: AggregationSpec{Type: AggregationCount},
		},
		{
			name: "count field prefix",
			text: "doc:count",
			want: AggregationSpec{Type: AggregationCount},
		},
		{
			name: "count uppercase",
			text: "DOC:COUNT",
			want: AggregationSpec{Type: AggregationCount},
		},
		{
			name: "terms default leaves size unset for the adapter",
			text: "type:terms",
			want: AggregationSpec{Type: AggregationTerm, Field: "type", Size: 0},
		},
		{
			name: "terms empty trailing size leaves size unset for the adapter",
			text: "type:terms:",
			want: AggregationSpec{Type: AggregationTerm, Field: "type", Size: 0},
		},
		{
			name: "terms explicit size",
			text: "type:TERMS:50",
			want: AggregationSpec{Type: AggregationTerm, Field: "type", Size: 50},
		},
		{
			name: "histogram default",
			text: "created_at:histogram",
			want: AggregationSpec{Type: AggregationHistogram, Field: "created_at", Interval: "month"},
		},
		{
			name: "histogram empty trailing interval",
			text: "created_at:histogram:",
			want: AggregationSpec{Type: AggregationHistogram, Field: "created_at", Interval: "month"},
		},
		{
			name: "histogram year interval",
			text: "created_at:HISTOGRAM:year",
			want: AggregationSpec{Type: AggregationHistogram, Field: "created_at", Interval: "year"},
		},
		{
			name: "histogram day interval",
			text: "created_at:histogram:day",
			want: AggregationSpec{Type: AggregationHistogram, Field: "created_at", Interval: "day"},
		},
		{
			name: "histogram week interval",
			text: "created_at:histogram:week",
			want: AggregationSpec{Type: AggregationHistogram, Field: "created_at", Interval: "week"},
		},
		{
			name: "range default",
			text: "line_count:range",
			want: AggregationSpec{Type: AggregationRange, Field: "line_count", Ranges: []string{"0-100", "100-500", "500-"}},
		},
		{
			name: "range empty trailing ranges",
			text: "line_count:range:",
			want: AggregationSpec{Type: AggregationRange, Field: "line_count", Ranges: []string{"0-100", "100-500", "500-"}},
		},
		{
			name: "range custom",
			text: "line_count:RANGE:0-10,10-",
			want: AggregationSpec{Type: AggregationRange, Field: "line_count", Ranges: []string{"0-10", "10-"}},
		},
		{
			name: "range custom multi-range",
			text: "line_count:range:0-50,50-100",
			want: AggregationSpec{Type: AggregationRange, Field: "line_count", Ranges: []string{"0-50", "50-100"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAggregation(tt.text)
			if err != nil {
				t.Fatalf("ParseAggregation(%q): %v", tt.text, err)
			}
			if got.Type != tt.want.Type || got.Field != tt.want.Field || got.Interval != tt.want.Interval || got.Size != tt.want.Size {
				t.Fatalf("spec = %#v, want %#v", got, tt.want)
			}
			if len(got.Ranges) != len(tt.want.Ranges) {
				t.Fatalf("ranges = %#v, want %#v", got.Ranges, tt.want.Ranges)
			}
			for i := range got.Ranges {
				if got.Ranges[i] != tt.want.Ranges[i] {
					t.Errorf("range %d = %q, want %q", i, got.Ranges[i], tt.want.Ranges[i])
				}
			}
		})
	}
}

func TestParseAggregationRejectsInvalidSpecs(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr string
	}{
		{
			name:    "empty spec",
			text:    "",
			wantErr: `invalid aggregation spec "": expected field:type[:interval]`,
		},
		{
			name:    "missing separator",
			text:    "invalid",
			wantErr: `invalid aggregation spec "invalid": expected field:type[:interval]`,
		},
		{
			name:    "field only",
			text:    "field",
			wantErr: `invalid aggregation spec "field": expected field:type[:interval]`,
		},
		{
			name:    "empty type",
			text:    "field:",
			wantErr: `unknown aggregation type ""`,
		},
		{
			name:    "unknown type simple",
			text:    "field:unknown",
			wantErr: `unknown aggregation type "unknown"`,
		},
		{
			name:    "unknown type explicit",
			text:    "field:unknown_type",
			wantErr: `unknown aggregation type "unknown_type"`,
		},
		{
			name:    "terms zero size",
			text:    "type:terms:0",
			wantErr: `invalid aggregation spec "type:terms:0": terms size must be a positive integer`,
		},
		{
			name:    "terms negative size",
			text:    "type:terms:-1",
			wantErr: `invalid aggregation spec "type:terms:-1": terms size must be a positive integer`,
		},
		{
			name:    "terms negative size large",
			text:    "type:terms:-5",
			wantErr: `invalid aggregation spec "type:terms:-5": terms size must be a positive integer`,
		},
		{
			name:    "terms non numeric size",
			text:    "type:terms:abc",
			wantErr: `invalid aggregation spec "type:terms:abc": terms size must be a positive integer`,
		},
		{
			name:    "terms alphanumeric size",
			text:    "type:terms:10extra",
			wantErr: `invalid aggregation spec "type:terms:10extra": terms size must be a positive integer`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAggregation(tt.text)
			if err == nil {
				t.Fatalf("ParseAggregation(%q) returned nil error, want %q", tt.text, tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("ParseAggregation(%q) error = %q, want %q", tt.text, err.Error(), tt.wantErr)
			}
		})
	}
}

// TestParseAggregationRangeDefaultIsCopied pins the defensive copy: mutating
// the returned spec's ranges must not corrupt the default bucket set applied
// to the next parse.
func TestParseAggregationRangeDefaultIsCopied(t *testing.T) {
	spec1, err := ParseAggregation("score:range")
	if err != nil {
		t.Fatalf("ParseAggregation: %v", err)
	}
	spec1.Ranges[0] = "MUTATED"

	spec2, err := ParseAggregation("score:range")
	if err != nil {
		t.Fatalf("ParseAggregation: %v", err)
	}
	if spec2.Ranges[0] != "0-100" {
		t.Errorf("mutating returned spec.Ranges corrupted the default bucket set: %q", spec2.Ranges[0])
	}
}
