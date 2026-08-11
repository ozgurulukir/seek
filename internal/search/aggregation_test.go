package search

import (
	"database/sql"
	"testing"
	"sort"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Create tables needed for aggregation tests
	_, err = db.Exec(`
		CREATE TABLE collections (
			id INTEGER PRIMARY KEY,
			name TEXT,
			type TEXT
		);
		CREATE TABLE documents (
			id INTEGER PRIMARY KEY,
			collection_id INTEGER,
			path TEXT,
			line_count INTEGER,
			created_at DATETIME
		);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	// Insert test data
	_, err = db.Exec(`
		INSERT INTO collections (id, name, type) VALUES
		(1, 'col1', 'typeA'),
		(2, 'col2', 'typeB');

		INSERT INTO documents (id, collection_id, path, line_count, created_at) VALUES
		(1, 1, 'path/1', 50, '2023-01-15T10:00:00Z'),
		(2, 1, 'path/2', 150, '2023-01-20T10:00:00Z'),
		(3, 2, 'path/3', 250, '2023-02-15T10:00:00Z'),
		(4, 2, 'path/4', 600, '2023-03-15T10:00:00Z');
	`)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	return db
}

func TestAggregation_Scan(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tests := []struct {
		name     string
		agg      Aggregation
		want     []Bucket
		wantErr  bool
	}{
		{
			name: "TermAggregation",
			agg:  &TermAggregation{Field: "type"},
			want: []Bucket{
				{Key: "typeA", Count: 2},
				{Key: "typeB", Count: 2},
			},
		},
		{
			name: "HistogramAggregation",
			agg:  &HistogramAggregation{Field: "created_at", Interval: "month"},
			want: []Bucket{
				{Key: "2023-01", Count: 2},
				{Key: "2023-02", Count: 1},
				{Key: "2023-03", Count: 1},
			},
		},
		{
			name: "RangeAggregation",
			agg:  &RangeAggregation{Field: "line_count", Ranges: []string{"0-100", "100-500", "500-"}},
			want: []Bucket{
				{Key: "0-100", Count: 1},
				{Key: "100-500", Count: 2},
				{Key: "500-", Count: 1},
			},
		},
		{
			name: "CountAggregation",
			agg:  &CountAggregation{},
			want: []Bucket{
				{Key: "count", Count: 4},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExecuteAggregation(db, tt.agg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteAggregation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("ExecuteAggregation() got length %v, want %v", len(got), len(tt.want))
				return
			}

			// Sort buckets for stable comparison
			sort.Slice(got, func(i, j int) bool {
				return got[i].Key < got[j].Key
			})
			sort.Slice(tt.want, func(i, j int) bool {
				return tt.want[i].Key < tt.want[j].Key
			})

			for i, b := range got {
				if b.Key != tt.want[i].Key || b.Count != tt.want[i].Count {
					t.Errorf("ExecuteAggregation()[%d] got = %v, want %v", i, b, tt.want[i])
				}
			}
		})
	}
}

func TestAggregation_SQL_Intervals(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		wantSQL  string
	}{
		{
			name:     "day interval",
			interval: "day",
			wantSQL:  "SELECT strftime('%Y-%m-%d', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			name:     "week interval",
			interval: "week",
			wantSQL:  "SELECT strftime('%Y-%W', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			name:     "month interval",
			interval: "month",
			wantSQL:  "SELECT strftime('%Y-%m', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			name:     "year interval",
			interval: "year",
			wantSQL:  "SELECT strftime('%Y', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			name:     "default interval",
			interval: "unknown",
			wantSQL:  "SELECT strftime('%Y-%m', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &HistogramAggregation{Field: "created_at", Interval: tt.interval}
			gotSQL, _ := agg.SQL()
			if gotSQL != tt.wantSQL {
				t.Errorf("HistogramAggregation.SQL() = %v, want %v", gotSQL, tt.wantSQL)
			}
		})
	}
}

func TestAggregation_SQL_Terms(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		wantSQL  string
	}{
		{
			name:     "type field",
			field:    "type",
			wantSQL:  "SELECT c.type as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY c.type ORDER BY count DESC",
		},
		{
			name:     "doc_type field",
			field:    "doc_type",
			wantSQL:  "SELECT c.type as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY c.type ORDER BY count DESC",
		},
		{
			name:     "collection field",
			field:    "collection",
			wantSQL:  "SELECT c.name as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY c.name ORDER BY count DESC",
		},
		{
			name:     "created_at field",
			field:    "created_at",
			wantSQL:  "SELECT d.created_at as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY d.created_at ORDER BY count DESC",
		},
		{
			name:     "date field",
			field:    "date",
			wantSQL:  "SELECT d.created_at as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY d.created_at ORDER BY count DESC",
		},
		{
			name:     "line_count field",
			field:    "line_count",
			wantSQL:  "SELECT d.line_count as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY d.line_count ORDER BY count DESC",
		},
		{
			name:     "path field",
			field:    "path",
			wantSQL:  "SELECT d.path as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY d.path ORDER BY count DESC",
		},
		{
			name:     "custom field without prefix",
			field:    "custom",
			wantSQL:  "SELECT c.custom as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY c.custom ORDER BY count DESC",
		},
		{
			name:     "custom field with prefix",
			field:    "d.custom",
			wantSQL:  "SELECT d.custom as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY d.custom ORDER BY count DESC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &TermAggregation{Field: tt.field}
			gotSQL, _ := agg.SQL()
			if gotSQL != tt.wantSQL {
				t.Errorf("TermAggregation.SQL() = %v, want %v", gotSQL, tt.wantSQL)
			}
		})
	}
}

func TestAggregation_SQL_Range(t *testing.T) {
	tests := []struct {
		name    string
		ranges  []string
		wantSQL string
	}{
		{
			name:    "single range open ended",
			ranges:  []string{"500-"},
			wantSQL: "SELECT CASE WHEN d.line_count >= 500 THEN '500-' ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			name:    "single range closed",
			ranges:  []string{"100-500"},
			wantSQL: "SELECT CASE WHEN d.line_count >= 100 AND d.line_count < 500 THEN '100-500' ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			name:    "multiple ranges",
			ranges:  []string{"0-100", "100-500", "500-"},
			wantSQL: "SELECT CASE WHEN d.line_count >= 0 AND d.line_count < 100 THEN '0-100' WHEN d.line_count >= 100 AND d.line_count < 500 THEN '100-500' WHEN d.line_count >= 500 THEN '500-' ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &RangeAggregation{Field: "line_count", Ranges: tt.ranges}
			gotSQL, _ := agg.SQL()
			if gotSQL != tt.wantSQL {
				t.Errorf("RangeAggregation.SQL() = %v, want %v", gotSQL, tt.wantSQL)
			}
		})
	}
}

func TestAggregation_SQL_Count(t *testing.T) {
	agg := &CountAggregation{}
	wantSQL := "SELECT COUNT(*) FROM documents d JOIN collections c ON c.id = d.collection_id"
	gotSQL, _ := agg.SQL()
	if gotSQL != wantSQL {
		t.Errorf("CountAggregation.SQL() = %v, want %v", gotSQL, wantSQL)
	}
}

func TestExecuteAggregation_Error(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Create a dummy aggregation that produces invalid SQL
	agg := &TermAggregation{Field: "non_existent_column"}

	// We might need to drop tables or make sure the query fails
	_, err := db.Exec("DROP TABLE documents")
	if err != nil {
		t.Fatalf("Failed to drop table for error test: %v", err)
	}

	_, err = ExecuteAggregation(db, agg)
	if err == nil {
		t.Error("ExecuteAggregation() expected error, got nil")
	}
}

func TestParseAggregation_ErrorPaths(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr bool
	}{
		{"invalid spec format", "type", true}, // too few parts and not "count"
		{"unknown aggregation type", "field:unknown", true},
		{"valid count", "count", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAggregation(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAggregation() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Add scan error test for all aggregation types
func TestAggregation_Scan_Error(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Create rows that will fail when scanned into int

	// TermAggregation
	t.Run("TermAggregation", func(t *testing.T) {
		rows, err := db.Query("SELECT 'key', 'not_a_number' as count FROM collections LIMIT 1")
		if err != nil {
			t.Fatalf("Failed to create invalid rows: %v", err)
		}
		agg := &TermAggregation{}
		_, err = agg.Scan(rows)
		if err == nil {
			t.Errorf("Expected scan error for TermAggregation, got nil")
		}
		defer rows.Close()
	})

	// HistogramAggregation
	t.Run("HistogramAggregation", func(t *testing.T) {
		rows, err := db.Query("SELECT 'key', 'not_a_number' as count FROM collections LIMIT 1")
		if err != nil {
			t.Fatalf("Failed to create invalid rows: %v", err)
		}
		agg := &HistogramAggregation{}
		_, err = agg.Scan(rows)
		if err == nil {
			t.Errorf("Expected scan error for HistogramAggregation, got nil")
		}
		defer rows.Close()
	})

	// RangeAggregation
	t.Run("RangeAggregation", func(t *testing.T) {
		rows, err := db.Query("SELECT 'key', 'not_a_number' as count FROM collections LIMIT 1")
		if err != nil {
			t.Fatalf("Failed to create invalid rows: %v", err)
		}
		agg := &RangeAggregation{}
		_, err = agg.Scan(rows)
		if err == nil {
			t.Errorf("Expected scan error for RangeAggregation, got nil")
		}
		defer rows.Close()
	})

	// CountAggregation
	t.Run("CountAggregation", func(t *testing.T) {
		rows, err := db.Query("SELECT 'not_a_number' as count FROM collections LIMIT 1")
		if err != nil {
			t.Fatalf("Failed to create invalid rows: %v", err)
		}
		agg := &CountAggregation{}
		_, err = agg.Scan(rows)
		if err == nil {
			t.Errorf("Expected scan error for CountAggregation, got nil")
		}
		defer rows.Close()
	})
}
