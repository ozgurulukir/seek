package search

import (
	"reflect"
	"testing"
)

func TestParseAggregationSpecs(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantType reflect.Type
		want     AggregationSpec
	}{
		{
			name:     "count simple",
			text:     "count",
			wantType: reflect.TypeOf(&CountAggregation{}),
			want:     AggregationSpec{Type: AggregationCount},
		},
		{
			name:     "count field prefix",
			text:     "doc:count",
			wantType: reflect.TypeOf(&CountAggregation{}),
			want:     AggregationSpec{Type: AggregationCount},
		},
		{
			name:     "count uppercase",
			text:     "DOC:COUNT",
			wantType: reflect.TypeOf(&CountAggregation{}),
			want:     AggregationSpec{Type: AggregationCount},
		},
		{
			name:     "terms default",
			text:     "type:terms",
			wantType: reflect.TypeOf(&TermAggregation{}),
			want:     AggregationSpec{Type: AggregationTerm, Field: "type", Size: DefaultAggregationBucketLimit},
		},
		{
			name:     "terms empty trailing size",
			text:     "type:terms:",
			wantType: reflect.TypeOf(&TermAggregation{}),
			want:     AggregationSpec{Type: AggregationTerm, Field: "type", Size: DefaultAggregationBucketLimit},
		},
		{
			name:     "terms explicit size",
			text:     "type:TERMS:50",
			wantType: reflect.TypeOf(&TermAggregation{}),
			want:     AggregationSpec{Type: AggregationTerm, Field: "type", Size: 50},
		},
		{
			name:     "histogram default",
			text:     "created_at:histogram",
			wantType: reflect.TypeOf(&HistogramAggregation{}),
			want:     AggregationSpec{Type: AggregationHistogram, Field: "created_at", Interval: "month"},
		},
		{
			name:     "histogram empty trailing interval",
			text:     "created_at:histogram:",
			wantType: reflect.TypeOf(&HistogramAggregation{}),
			want:     AggregationSpec{Type: AggregationHistogram, Field: "created_at", Interval: "month"},
		},
		{
			name:     "histogram interval",
			text:     "created_at:HISTOGRAM:year",
			wantType: reflect.TypeOf(&HistogramAggregation{}),
			want:     AggregationSpec{Type: AggregationHistogram, Field: "created_at", Interval: "year"},
		},
		{
			name:     "range default",
			text:     "line_count:range",
			wantType: reflect.TypeOf(&RangeAggregation{}),
			want:     AggregationSpec{Type: AggregationRange, Field: "line_count", Ranges: []string{"0-100", "100-500", "500-"}},
		},
		{
			name:     "range empty trailing ranges",
			text:     "line_count:range:",
			wantType: reflect.TypeOf(&RangeAggregation{}),
			want:     AggregationSpec{Type: AggregationRange, Field: "line_count", Ranges: []string{"0-100", "100-500", "500-"}},
		},
		{
			name:     "range custom",
			text:     "line_count:RANGE:0-10,10-",
			wantType: reflect.TypeOf(&RangeAggregation{}),
			want:     AggregationSpec{Type: AggregationRange, Field: "line_count", Ranges: []string{"0-10", "10-"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aggregation, err := ParseAggregation(tt.text)
			if err != nil {
				t.Fatalf("ParseAggregation(%q): %v", tt.text, err)
			}
			if gotType := reflect.TypeOf(aggregation); gotType != tt.wantType {
				t.Errorf("ParseAggregation(%q) type = %v, want %v", tt.text, gotType, tt.wantType)
			}
			got := aggregation.Spec()
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
	invalid := []string{
		"",
		"invalid",
		"field",
		"field:",
		"field:unknown",
		"type:terms:0",
		"type:terms:-1",
		"type:terms:-5",
		"type:terms:abc",
		"type:terms:10extra",
	}
	for _, text := range invalid {
		t.Run(text, func(t *testing.T) {
			if _, err := ParseAggregation(text); err == nil {
				t.Fatalf("ParseAggregation(%q) returned nil error", text)
			}
		})
	}
}

func TestTermAggregationSpec(t *testing.T) {
	for _, tc := range []struct {
		name      string
		agg       TermAggregation
		wantType  AggregationType
		wantField string
		wantSize  int
	}{
		{
			name:      "zero size default limit",
			agg:       TermAggregation{Field: "category"},
			wantType:  AggregationTerm,
			wantField: "category",
			wantSize:  DefaultAggregationBucketLimit,
		},
		{
			name:      "negative size default limit",
			agg:       TermAggregation{Field: "tag", Size: -3},
			wantType:  AggregationTerm,
			wantField: "tag",
			wantSize:  DefaultAggregationBucketLimit,
		},
		{
			name:      "explicit size",
			agg:       TermAggregation{Field: "author", Size: 25},
			wantType:  AggregationTerm,
			wantField: "author",
			wantSize:  25,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := tc.agg.Spec()
			if spec.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", spec.Type, tc.wantType)
			}
			if spec.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", spec.Field, tc.wantField)
			}
			if spec.Size != tc.wantSize {
				t.Errorf("Size = %d, want %d", spec.Size, tc.wantSize)
			}
		})
	}
}

func TestHistogramAggregationSpec(t *testing.T) {
	for _, tc := range []struct {
		name         string
		agg          HistogramAggregation
		wantType     AggregationType
		wantField    string
		wantInterval string
	}{
		{
			name:         "default interval",
			agg:          HistogramAggregation{Field: "created_at"},
			wantType:     AggregationHistogram,
			wantField:    "created_at",
			wantInterval: "month",
		},
		{
			name:         "explicit interval",
			agg:          HistogramAggregation{Field: "created_at", Interval: "year"},
			wantType:     AggregationHistogram,
			wantField:    "created_at",
			wantInterval: "year",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := tc.agg.Spec()
			if spec.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", spec.Type, tc.wantType)
			}
			if spec.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", spec.Field, tc.wantField)
			}
			if spec.Interval != tc.wantInterval {
				t.Errorf("Interval = %q, want %q", spec.Interval, tc.wantInterval)
			}
		})
	}
}

func TestRangeAggregationSpec(t *testing.T) {
	for _, tc := range []struct {
		name       string
		agg        RangeAggregation
		wantType   AggregationType
		wantField  string
		wantRanges []string
	}{
		{
			name:       "default ranges",
			agg:        RangeAggregation{Field: "line_count"},
			wantType:   AggregationRange,
			wantField:  "line_count",
			wantRanges: []string{"0-100", "100-500", "500-"},
		},
		{
			name:       "explicit ranges",
			agg:        RangeAggregation{Field: "line_count", Ranges: []string{"0-10", "10-"}},
			wantType:   AggregationRange,
			wantField:  "line_count",
			wantRanges: []string{"0-10", "10-"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := tc.agg.Spec()
			if spec.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", spec.Type, tc.wantType)
			}
			if spec.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", spec.Field, tc.wantField)
			}
			if len(spec.Ranges) != len(tc.wantRanges) {
				t.Fatalf("len(Ranges) = %d, want %d", len(spec.Ranges), len(tc.wantRanges))
			}
			for i := range spec.Ranges {
				if spec.Ranges[i] != tc.wantRanges[i] {
					t.Errorf("Ranges[%d] = %q, want %q", i, spec.Ranges[i], tc.wantRanges[i])
				}
			}
		})
	}
}

func TestCountAggregationSpec(t *testing.T) {
	agg := CountAggregation{}
	spec := agg.Spec()
	if spec.Type != AggregationCount {
		t.Errorf("Type = %q, want %q", spec.Type, AggregationCount)
	}
}
