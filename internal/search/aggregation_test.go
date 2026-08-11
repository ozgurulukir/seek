package search

import (
	"database/sql"
	"reflect"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestTermAggregation_Scan(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	defer db.Close()

	// Query that returns rows matching the expected structure (key string, count int)
	rows, err := db.Query("SELECT 'hello' as key, 42 as count UNION ALL SELECT 'world' as key, 10 as count")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	agg := &TermAggregation{Field: "type"}
	buckets, err := agg.Scan(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []Bucket{
		{Key: "hello", Count: 42},
		{Key: "world", Count: 10},
	}

	if !reflect.DeepEqual(buckets, expected) {
		t.Errorf("got %v, want %v", buckets, expected)
	}
}

func TestTermAggregation_ScanError(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	defer db.Close()

	// Query with incorrect column types (int for key, string for count etc - or maybe just wrong number of columns)
	// TermAggregation expects 2 columns
	rows, err := db.Query("SELECT 'hello' as key") // Only one column
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	agg := &TermAggregation{Field: "type"}
	_, err = agg.Scan(rows)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestTermAggregation_SQL(t *testing.T) {
	tests := []struct {
		field    string
		wantSQL  string
	}{
		{
			field:   "type",
			wantSQL: "SELECT c.type as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY c.type ORDER BY count DESC",
		},
		{
			field:   "doc_type",
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
			field:   "date",
			wantSQL: "SELECT d.created_at as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY d.created_at ORDER BY count DESC",
		},
		{
			field:   "line_count",
			wantSQL: "SELECT d.line_count as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY d.line_count ORDER BY count DESC",
		},
		{
			field:   "path",
			wantSQL: "SELECT d.path as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY d.path ORDER BY count DESC",
		},
		{
			field:   "custom_field",
			wantSQL: "SELECT c.custom_field as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY c.custom_field ORDER BY count DESC",
		},
		{
			field:   "d.custom_field",
			wantSQL: "SELECT d.custom_field as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY d.custom_field ORDER BY count DESC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			agg := &TermAggregation{Field: tt.field}
			query, _ := agg.SQL()
			if query != tt.wantSQL {
				t.Errorf("got %q, want %q", query, tt.wantSQL)
			}
		})
	}
}

func TestExecuteAggregation(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	defer db.Close()

	// Set up schema and dummy data
	_, err = db.Exec(`
		CREATE TABLE collections (id INTEGER PRIMARY KEY, type TEXT, name TEXT);
		CREATE TABLE documents (id INTEGER PRIMARY KEY, collection_id INTEGER, created_at TEXT, line_count INTEGER, path TEXT);
		INSERT INTO collections (id, type, name) VALUES (1, 'markdown', 'docs');
		INSERT INTO collections (id, type, name) VALUES (2, 'claude', 'chats');
		INSERT INTO documents (id, collection_id) VALUES (1, 1);
		INSERT INTO documents (id, collection_id) VALUES (2, 1);
		INSERT INTO documents (id, collection_id) VALUES (3, 2);
	`)
	if err != nil {
		t.Fatalf("failed to create schema and dummy data: %v", err)
	}

	agg := &TermAggregation{Field: "type"}
	buckets, err := ExecuteAggregation(db, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []Bucket{
		{Key: "markdown", Count: 2},
		{Key: "claude", Count: 1},
	}

	if !reflect.DeepEqual(buckets, expected) {
		t.Errorf("got %v, want %v", buckets, expected)
	}
}

// Add similar tests for other aggregations: HistogramAggregation, RangeAggregation, CountAggregation

func TestHistogramAggregation_Scan(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT '2023-01' as key, 42 as count UNION ALL SELECT '2023-02' as key, 10 as count")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	agg := &HistogramAggregation{Field: "created_at", Interval: "month"}
	buckets, err := agg.Scan(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []Bucket{
		{Key: "2023-01", Count: 42},
		{Key: "2023-02", Count: 10},
	}

	if !reflect.DeepEqual(buckets, expected) {
		t.Errorf("got %v, want %v", buckets, expected)
	}
}

func TestRangeAggregation_Scan(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT '0-100' as key, 42 as count UNION ALL SELECT '100-500' as key, 10 as count")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	agg := &RangeAggregation{Field: "line_count", Ranges: []string{"0-100", "100-500"}}
	buckets, err := agg.Scan(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []Bucket{
		{Key: "0-100", Count: 42},
		{Key: "100-500", Count: 10},
	}

	if !reflect.DeepEqual(buckets, expected) {
		t.Errorf("got %v, want %v", buckets, expected)
	}
}

func TestCountAggregation_Scan(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	defer db.Close()

	// CountAggregation queries just the count
	rows, err := db.Query("SELECT 42 as count")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	agg := &CountAggregation{}
	buckets, err := agg.Scan(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []Bucket{
		{Key: "count", Count: 42},
	}

	if !reflect.DeepEqual(buckets, expected) {
		t.Errorf("got %v, want %v", buckets, expected)
	}
}
