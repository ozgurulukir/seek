package search

import (
	"database/sql"
	"reflect"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestRangeAggregation_Scan(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE agg_results (key TEXT, count INTEGER)")
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	_, err = db.Exec("INSERT INTO agg_results (key, count) VALUES ('0-100', 5), ('100-500', 10), ('500-', 3)")
	if err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	rows, err := db.Query("SELECT key, count FROM agg_results ORDER BY count ASC")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	agg := &RangeAggregation{}
	buckets, err := agg.Scan(rows)
	if err != nil {
		t.Fatalf("Scan returned unexpected error: %v", err)
	}

	expected := []Bucket{
		{Key: "500-", Count: 3},
		{Key: "0-100", Count: 5},
		{Key: "100-500", Count: 10},
	}

	if !reflect.DeepEqual(buckets, expected) {
		t.Errorf("expected buckets %v, got %v", expected, buckets)
	}
}

func TestRangeAggregation_Scan_Error(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Create table with only one column to cause a scan error
	_, err = db.Exec("CREATE TABLE agg_bad (key TEXT)")
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	_, err = db.Exec("INSERT INTO agg_bad (key) VALUES ('0-100')")
	if err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	rows, err := db.Query("SELECT key FROM agg_bad")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	agg := &RangeAggregation{}
	_, err = agg.Scan(rows)
	if err == nil {
		t.Fatal("expected error from Scan, got nil")
	}
}
