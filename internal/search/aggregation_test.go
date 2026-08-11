package search

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestCountAggregation_Scan(t *testing.T) {
	// We'll test CountAggregation.Scan, TermAggregation.Scan, HistogramAggregation.Scan and RangeAggregation.Scan using an in-memory db
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer db.Close()

	// 1. Test happy path for CountAggregation
	t.Run("CountAggregation Happy Path", func(t *testing.T) {
		rows, err := db.Query("SELECT 42")
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		defer rows.Close()

		agg := &CountAggregation{}
		buckets, err := agg.Scan(rows)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(buckets) != 1 {
			t.Errorf("expected 1 bucket, got %d", len(buckets))
		} else if buckets[0].Key != "count" || buckets[0].Count != 42 {
			t.Errorf("expected bucket {Key: \"count\", Count: 42}, got %+v", buckets[0])
		}
	})

	// 2. Test error path for CountAggregation
	t.Run("CountAggregation Scan Error", func(t *testing.T) {
		rows, err := db.Query("SELECT 'not-an-int'")
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		defer rows.Close()

		agg := &CountAggregation{}
		_, err = agg.Scan(rows)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	// Let's add similar tests for the other aggregations while we are at it

	// TermAggregation
	t.Run("TermAggregation Happy Path", func(t *testing.T) {
		rows, err := db.Query("SELECT 'term1', 10 UNION ALL SELECT 'term2', 5")
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		defer rows.Close()

		agg := &TermAggregation{Field: "f1"}
		buckets, err := agg.Scan(rows)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(buckets) != 2 {
			t.Errorf("expected 2 buckets, got %d", len(buckets))
		} else {
			if buckets[0].Key != "term1" || buckets[0].Count != 10 {
				t.Errorf("expected bucket {Key: \"term1\", Count: 10}, got %+v", buckets[0])
			}
			if buckets[1].Key != "term2" || buckets[1].Count != 5 {
				t.Errorf("expected bucket {Key: \"term2\", Count: 5}, got %+v", buckets[1])
			}
		}
	})

	t.Run("TermAggregation Scan Error", func(t *testing.T) {
		rows, err := db.Query("SELECT 'term1', 'not-an-int'")
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		defer rows.Close()

		agg := &TermAggregation{}
		_, err = agg.Scan(rows)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	// HistogramAggregation
	t.Run("HistogramAggregation Happy Path", func(t *testing.T) {
		rows, err := db.Query("SELECT '2023-01-01', 10 UNION ALL SELECT '2023-01-02', 5")
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		defer rows.Close()

		agg := &HistogramAggregation{}
		buckets, err := agg.Scan(rows)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(buckets) != 2 {
			t.Errorf("expected 2 buckets, got %d", len(buckets))
		}
	})

	t.Run("HistogramAggregation Scan Error", func(t *testing.T) {
		rows, err := db.Query("SELECT '2023-01-01', 'not-an-int'")
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		defer rows.Close()

		agg := &HistogramAggregation{}
		_, err = agg.Scan(rows)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	// RangeAggregation
	t.Run("RangeAggregation Happy Path", func(t *testing.T) {
		rows, err := db.Query("SELECT '0-100', 10 UNION ALL SELECT '100-500', 5")
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		defer rows.Close()

		agg := &RangeAggregation{}
		buckets, err := agg.Scan(rows)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(buckets) != 2 {
			t.Errorf("expected 2 buckets, got %d", len(buckets))
		}
	})

	t.Run("RangeAggregation Scan Error", func(t *testing.T) {
		rows, err := db.Query("SELECT '0-100', 'not-an-int'")
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		defer rows.Close()

		agg := &RangeAggregation{}
		_, err = agg.Scan(rows)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestAggregation_SQL(t *testing.T) {
	tests := []struct {
		name      string
		agg       Aggregation
		wantQuery string
		wantArgs  int // expected number of args
	}{
		{
			name:      "TermAggregation with generic field",
			agg:       &TermAggregation{Field: "custom_field"},
			wantQuery: "SELECT c.custom_field as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY c.custom_field ORDER BY count DESC",
		},
		{
			name:      "TermAggregation with type field",
			agg:       &TermAggregation{Field: "type"},
			wantQuery: "SELECT c.type as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY c.type ORDER BY count DESC",
		},
		{
			name:      "HistogramAggregation with default interval",
			agg:       &HistogramAggregation{Field: "created_at"},
			wantQuery: "SELECT strftime('%Y-%m', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			name:      "HistogramAggregation with week interval",
			agg:       &HistogramAggregation{Field: "created_at", Interval: "week"},
			wantQuery: "SELECT strftime('%Y-%W', d.created_at) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			name:      "RangeAggregation",
			agg:       &RangeAggregation{Field: "line_count", Ranges: []string{"0-100", "100-"}},
			wantQuery: "SELECT CASE WHEN d.line_count >= 0 AND d.line_count < 100 THEN '0-100' WHEN d.line_count >= 100 THEN '100-' ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key",
		},
		{
			name:      "CountAggregation",
			agg:       &CountAggregation{},
			wantQuery: "SELECT COUNT(*) FROM documents d JOIN collections c ON c.id = d.collection_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, args := tt.agg.SQL()
			if query != tt.wantQuery {
				t.Errorf("expected query %q, got %q", tt.wantQuery, query)
			}
			if len(args) != tt.wantArgs {
				t.Errorf("expected %d args, got %d", tt.wantArgs, len(args))
			}
		})
	}
}

func TestExecuteAggregation(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer db.Close()

	// Setup schema and some data
	_, err = db.Exec(`
		CREATE TABLE collections (id INTEGER PRIMARY KEY, name TEXT, type TEXT);
		CREATE TABLE documents (id INTEGER PRIMARY KEY, collection_id INTEGER, created_at DATETIME, line_count INTEGER, path TEXT);
		INSERT INTO collections (id, name, type) VALUES (1, 'col1', 'type1');
		INSERT INTO documents (collection_id, created_at, line_count, path) VALUES (1, '2023-01-01', 50, 'p1');
		INSERT INTO documents (collection_id, created_at, line_count, path) VALUES (1, '2023-01-02', 150, 'p2');
	`)
	if err != nil {
		t.Fatalf("failed to setup db: %v", err)
	}

	agg := &CountAggregation{}
	buckets, err := ExecuteAggregation(db, agg)
	if err != nil {
		t.Fatalf("ExecuteAggregation error: %v", err)
	}

	if len(buckets) != 1 || buckets[0].Count != 2 {
		t.Errorf("expected count 2, got %+v", buckets)
	}

	// Invalid SQL tests
	errAgg := &TermAggregation{Field: "invalid syntax field"}
	// Drop tables or cause error in query
	_, err = db.Exec("DROP TABLE documents")
	if err != nil {
		t.Fatalf("failed to drop table: %v", err)
	}

	_, err = ExecuteAggregation(db, errAgg)
	if err == nil {
		t.Errorf("expected ExecuteAggregation to fail")
	}
}
