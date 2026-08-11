package search

import (
	"database/sql"
	"reflect"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

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
			created_at TEXT,
			line_count INTEGER
		);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO collections (id, name, type) VALUES
		(1, 'col1', 'markdown'),
		(2, 'col2', 'pdf');

		INSERT INTO documents (id, collection_id, path, created_at, line_count) VALUES
		(1, 1, '/doc1.md', '2023-01-15T10:00:00Z', 50),
		(2, 1, '/doc2.md', '2023-01-20T10:00:00Z', 150),
		(3, 2, '/doc3.pdf', '2023-02-10T10:00:00Z', 300),
		(4, 2, '/doc4.pdf', '2023-03-05T10:00:00Z', 600),
		(5, 1, '/doc5.md', '2023-01-25T10:00:00Z', 20);
	`)
	if err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	return db
}

func TestExecuteAggregation_Term(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	agg := &TermAggregation{Field: "type"}
	buckets, err := ExecuteAggregation(db, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []Bucket{
		{Key: "markdown", Count: 3},
		{Key: "pdf", Count: 2},
	}

	if !reflect.DeepEqual(buckets, expected) {
		t.Errorf("expected %v, got %v", expected, buckets)
	}

	aggCol := &TermAggregation{Field: "collection"}
	bucketsCol, err := ExecuteAggregation(db, aggCol)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedCol := []Bucket{
		{Key: "col1", Count: 3},
		{Key: "col2", Count: 2},
	}

	if !reflect.DeepEqual(bucketsCol, expectedCol) {
		t.Errorf("expected %v, got %v", expectedCol, bucketsCol)
	}
}

func TestExecuteAggregation_Histogram(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	agg := &HistogramAggregation{Field: "created_at", Interval: "month"}
	buckets, err := ExecuteAggregation(db, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []Bucket{
		{Key: "2023-01", Count: 3},
		{Key: "2023-02", Count: 1},
		{Key: "2023-03", Count: 1},
	}

	if !reflect.DeepEqual(buckets, expected) {
		t.Errorf("expected %v, got %v", expected, buckets)
	}
}

func TestExecuteAggregation_Range(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	agg := &RangeAggregation{Field: "line_count", Ranges: []string{"0-100", "100-500", "500-"}}
	buckets, err := ExecuteAggregation(db, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Order by key string.
	// The CASE statement output will be '0-100', '100-500', '500-'.
	// These will be sorted lexicographically by SQLite ORDER BY key.
	// '0-100', '100-500', '500-'
	expected := []Bucket{
		{Key: "0-100", Count: 2},   // doc1 (50), doc5 (20)
		{Key: "100-500", Count: 2}, // doc2 (150), doc3 (300)
		{Key: "500-", Count: 1},    // doc4 (600)
	}

	if !reflect.DeepEqual(buckets, expected) {
		t.Errorf("expected %v, got %v", expected, buckets)
	}
}

func TestExecuteAggregation_Count(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	agg := &CountAggregation{}
	buckets, err := ExecuteAggregation(db, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []Bucket{
		{Key: "count", Count: 5},
	}

	if !reflect.DeepEqual(buckets, expected) {
		t.Errorf("expected %v, got %v", expected, buckets)
	}
}

type badAggregation struct{}
func (a *badAggregation) SQL() (string, []interface{}) {
	return "SELECT * FROM non_existent_table", nil
}
func (a *badAggregation) Scan(rows *sql.Rows) ([]Bucket, error) {
	return nil, nil
}

func TestExecuteAggregation_Error(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	agg := &badAggregation{}
	_, err := ExecuteAggregation(db, agg)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}
