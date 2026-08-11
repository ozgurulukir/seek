package search

import (
	"reflect"
	"testing"
)

func TestRangeAggregation_SQL(t *testing.T) {
	tests := []struct {
		name     string
		ranges   []string
		wantSQL  string
		wantArgs []interface{}
	}{
		{
			name:   "single range",
			ranges: []string{"0-100"},
			wantSQL: "SELECT CASE WHEN d.line_count >= 0 AND d.line_count < 100 THEN '0-100' ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
			wantArgs: nil,
		},
		{
			name:   "multiple ranges",
			ranges: []string{"0-100", "100-500", "500-"},
			wantSQL: "SELECT CASE WHEN d.line_count >= 0 AND d.line_count < 100 THEN '0-100' WHEN d.line_count >= 100 AND d.line_count < 500 THEN '100-500' WHEN d.line_count >= 500 THEN '500-' ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
			wantArgs: nil,
		},
		{
			name:   "invalid range format ignored",
			ranges: []string{"invalid", "0-100"},
			wantSQL: "SELECT CASE WHEN d.line_count >= 0 AND d.line_count < 100 THEN '0-100' ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
			wantArgs: nil,
		},
		{
			name:   "empty ranges",
			ranges: []string{},
			wantSQL: "SELECT CASE  ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
			wantArgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &RangeAggregation{
				Field:  "line_count",
				Ranges: tt.ranges,
			}
			gotSQL, gotArgs := a.SQL()
			if gotSQL != tt.wantSQL {
				t.Errorf("SQL() gotSQL = %v, want %v", gotSQL, tt.wantSQL)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("SQL() gotArgs = %v, want %v", gotArgs, tt.wantArgs)
			}
		})
	}
}

func TestTermAggregation_SQL(t *testing.T) {
	tests := []struct {
		field   string
		wantSQL string
	}{
		{
			field:   "type",
			wantSQL: "SELECT c.type as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY c.type ORDER BY count DESC",
		},
		{
			field:   "collection",
			wantSQL: "SELECT c.name as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY c.name ORDER BY count DESC",
		},
		{
			field:   "created_at",
			wantSQL: "SELECT d.created_at as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY d.created_at ORDER BY count DESC",
		},
		{
			field:   "custom_field",
			wantSQL: "SELECT c.custom_field as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY c.custom_field ORDER BY count DESC",
		},
		{
			field:   "d.custom",
			wantSQL: "SELECT d.custom as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY d.custom ORDER BY count DESC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			a := &TermAggregation{Field: tt.field}
			gotSQL, _ := a.SQL()
			if gotSQL != tt.wantSQL {
				t.Errorf("SQL() gotSQL = %v, want %v", gotSQL, tt.wantSQL)
			}
		})
	}
}

func TestHistogramAggregation_SQL(t *testing.T) {
	tests := []struct {
		interval string
		wantSQL  string
	}{
		{
			interval: "day",
			wantSQL:  "SELECT strftime('%Y-%m-%d', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			interval: "week",
			wantSQL:  "SELECT strftime('%Y-%W', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			interval: "month",
			wantSQL:  "SELECT strftime('%Y-%m', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			interval: "year",
			wantSQL:  "SELECT strftime('%Y', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			interval: "unknown",
			wantSQL:  "SELECT strftime('%Y-%m', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.interval, func(t *testing.T) {
			a := &HistogramAggregation{Field: "created_at", Interval: tt.interval}
			gotSQL, _ := a.SQL()
			if gotSQL != tt.wantSQL {
				t.Errorf("SQL() gotSQL = %v, want %v", gotSQL, tt.wantSQL)
			}
		})
	}
}

func TestCountAggregation_SQL(t *testing.T) {
	a := &CountAggregation{}
	gotSQL, _ := a.SQL()
	want := "SELECT COUNT(*) FROM documents d JOIN collections c ON c.id = d.collection_id"
	if gotSQL != want {
		t.Errorf("SQL() gotSQL = %v, want %v", gotSQL, want)
	}
}

func TestParseAggregation(t *testing.T) {
	tests := []struct {
		spec    string
		want    Aggregation
		wantErr bool
	}{
		{
			spec: "type:terms",
			want: &TermAggregation{Field: "type"},
		},
		{
			spec: "created_at:histogram",
			want: &HistogramAggregation{Field: "created_at", Interval: "month"},
		},
		{
			spec: "created_at:histogram:year",
			want: &HistogramAggregation{Field: "created_at", Interval: "year"},
		},
		{
			spec: "line_count:range",
			want: &RangeAggregation{Field: "line_count", Ranges: []string{"0-100", "100-500", "500-"}},
		},
		{
			spec: "line_count:range:0-10,10-20",
			want: &RangeAggregation{Field: "line_count", Ranges: []string{"0-10", "10-20"}},
		},
		{
			spec: "count",
			want: &CountAggregation{},
		},
		{
			spec:    "invalid",
			wantErr: true,
		},
		{
			spec:    "field:unknown",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := ParseAggregation(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAggregation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseAggregation() got = %v, want %v", got, tt.want)
			}
		})
	}
}
