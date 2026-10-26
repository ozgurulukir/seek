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
			name:     "histogram year interval",
			text:     "created_at:HISTOGRAM:year",
			wantType: reflect.TypeOf(&HistogramAggregation{}),
			want:     AggregationSpec{Type: AggregationHistogram, Field: "created_at", Interval: "year"},
		},
		{
			name:     "histogram day interval",
			text:     "created_at:histogram:day",
			wantType: reflect.TypeOf(&HistogramAggregation{}),
			want:     AggregationSpec{Type: AggregationHistogram, Field: "created_at", Interval: "day"},
		},
		{
			name:     "histogram week interval",
			text:     "created_at:histogram:week",
			wantType: reflect.TypeOf(&HistogramAggregation{}),
			want:     AggregationSpec{Type: AggregationHistogram, Field: "created_at", Interval: "week"},
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
		{
			name:     "range custom multi-range",
			text:     "line_count:range:0-50,50-100",
			wantType: reflect.TypeOf(&RangeAggregation{}),
			want:     AggregationSpec{Type: AggregationRange, Field: "line_count", Ranges: []string{"0-50", "50-100"}},
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

func TestParseAggregationStructFields(t *testing.T) {
	t.Run("histogram default fallback struct fields", func(t *testing.T) {
		agg, err := ParseAggregation("created_at:histogram")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h, ok := agg.(*HistogramAggregation)
		if !ok {
			t.Fatalf("expected *HistogramAggregation, got %T", agg)
		}
		if h.Field != "created_at" {
			t.Errorf("Field = %q, want %q", h.Field, "created_at")
		}
		if h.Interval != "month" {
			t.Errorf("Interval = %q, want %q", h.Interval, "month")
		}
	})

	t.Run("histogram empty trailing interval struct fields", func(t *testing.T) {
		agg, err := ParseAggregation("created_at:histogram:")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h, ok := agg.(*HistogramAggregation)
		if !ok {
			t.Fatalf("expected *HistogramAggregation, got %T", agg)
		}
		if h.Interval != "month" {
			t.Errorf("Interval = %q, want %q", h.Interval, "month")
		}
	})

	t.Run("histogram custom interval struct fields", func(t *testing.T) {
		agg, err := ParseAggregation("created_at:histogram:day")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h, ok := agg.(*HistogramAggregation)
		if !ok {
			t.Fatalf("expected *HistogramAggregation, got %T", agg)
		}
		if h.Interval != "day" {
			t.Errorf("Interval = %q, want %q", h.Interval, "day")
		}
	})

	t.Run("range default fallback struct fields", func(t *testing.T) {
		agg, err := ParseAggregation("line_count:range")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		r, ok := agg.(*RangeAggregation)
		if !ok {
			t.Fatalf("expected *RangeAggregation, got %T", agg)
		}
		if r.Field != "line_count" {
			t.Errorf("Field = %q, want %q", r.Field, "line_count")
		}
		wantRanges := []string{"0-100", "100-500", "500-"}
		if !reflect.DeepEqual(r.Ranges, wantRanges) {
			t.Errorf("Ranges = %#v, want %#v", r.Ranges, wantRanges)
		}
	})

	t.Run("range empty trailing ranges struct fields", func(t *testing.T) {
		agg, err := ParseAggregation("line_count:range:")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		r, ok := agg.(*RangeAggregation)
		if !ok {
			t.Fatalf("expected *RangeAggregation, got %T", agg)
		}
		wantRanges := []string{"0-100", "100-500", "500-"}
		if !reflect.DeepEqual(r.Ranges, wantRanges) {
			t.Errorf("Ranges = %#v, want %#v", r.Ranges, wantRanges)
		}
	})

	t.Run("range custom ranges struct fields", func(t *testing.T) {
		agg, err := ParseAggregation("line_count:range:0-10,10-50,50-")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		r, ok := agg.(*RangeAggregation)
		if !ok {
			t.Fatalf("expected *RangeAggregation, got %T", agg)
		}
		wantRanges := []string{"0-10", "10-50", "50-"}
		if !reflect.DeepEqual(r.Ranges, wantRanges) {
			t.Errorf("Ranges = %#v, want %#v", r.Ranges, wantRanges)
		}
	})

	t.Run("terms default fallback struct fields", func(t *testing.T) {
		agg, err := ParseAggregation("category:terms")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		term, ok := agg.(*TermAggregation)
		if !ok {
			t.Fatalf("expected *TermAggregation, got %T", agg)
		}
		if term.Field != "category" {
			t.Errorf("Field = %q, want %q", term.Field, "category")
		}
		if term.Size != 0 {
			t.Errorf("Size = %d, want 0", term.Size)
		}
	})

	t.Run("terms empty trailing size struct fields", func(t *testing.T) {
		agg, err := ParseAggregation("category:terms:")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		term, ok := agg.(*TermAggregation)
		if !ok {
			t.Fatalf("expected *TermAggregation, got %T", agg)
		}
		if term.Size != 0 {
			t.Errorf("Size = %d, want 0", term.Size)
		}
	})

	t.Run("terms explicit size struct fields", func(t *testing.T) {
		agg, err := ParseAggregation("category:terms:20")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		term, ok := agg.(*TermAggregation)
		if !ok {
			t.Fatalf("expected *TermAggregation, got %T", agg)
		}
		if term.Size != 20 {
			t.Errorf("Size = %d, want 20", term.Size)
		}
	})
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
