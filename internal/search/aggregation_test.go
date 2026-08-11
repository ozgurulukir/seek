package search

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// This file consolidates the unique, non-conflicting aggregation test coverage
// from PRs #8, #9, #14, #15, #16, #20, #21, #26, #27, and #30. Overlapping
// tests that asserted the pre-#17/pre-#23 SQL format (TermAggregation_SQL,
// RangeAggregation_SQL) are dropped — they are already covered by the
// TestTermAggregationSQL / TestRangeAggregationSQL functions in query_test.go
// which assert the current parameterized/quoted format.

// ------------------------------------------------------------------
// setupTestDB (from PR #15)
// ------------------------------------------------------------------

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Create tables.
	stmts := []string{
		`CREATE TABLE collections (
			id INTEGER PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			type TEXT NOT NULL,
			path TEXT NOT NULL,
			created_at TEXT
		)`,
		`CREATE TABLE documents (
			id INTEGER PRIMARY KEY,
			collection_id INTEGER REFERENCES collections(id),
			path TEXT NOT NULL,
			line_count INTEGER,
			created_at TEXT
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}

	// Insert seed data.
	_, err = db.Exec(`
		INSERT INTO collections (id, name, type, path, created_at) VALUES
		(1, 'docs', 'text', '/docs', '2023-01-01'),
		(2, 'src', 'code', '/src', '2023-01-02');
	`)
	if err != nil {
		t.Fatalf("failed to insert collections: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO documents (id, collection_id, path, line_count, created_at) VALUES
		(1, 1, 'readme.md', 50, '2023-01-15 10:00:00'),
		(2, 1, 'guide.md', 150, '2023-02-20 10:00:00'),
		(3, 2, 'main.go', 600, '2023-02-25 10:00:00'),
		(4, 2, 'utils.go', 50, '2023-03-05 10:00:00'),
		(5, 2, 'test.go', 200, '2023-03-10 10:00:00');
	`)
	if err != nil {
		t.Fatalf("failed to insert documents: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// ------------------------------------------------------------------
// TestCountAggregation_Scan (from PR #26) — omnibus Scan coverage
// ------------------------------------------------------------------

func TestCountAggregation_Scan(t *testing.T) {
	// Covers CountAggregation.Scan, TermAggregation.Scan,
	// HistogramAggregation.Scan and RangeAggregation.Scan using an in-memory db.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer db.Close()

	// 1. Test happy path for CountAggregation.
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

	// 2. Test error path for CountAggregation.
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

	// TermAggregation.
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

	// HistogramAggregation.
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

	// RangeAggregation.
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

// ------------------------------------------------------------------
// TestHistogramAggregation_SQL (from PR #14)
// ------------------------------------------------------------------

func TestHistogramAggregation_SQL(t *testing.T) {
	tests := []struct {
		name           string
		interval       string
		expectedFormat string
	}{
		{name: "day interval", interval: "day", expectedFormat: "%Y-%m-%d"},
		{name: "week interval", interval: "week", expectedFormat: "%Y-%W"},
		{name: "month interval", interval: "month", expectedFormat: "%Y-%m"},
		{name: "year interval", interval: "year", expectedFormat: "%Y"},
		{name: "default interval (empty)", interval: "", expectedFormat: "%Y-%m"},
		{name: "unknown interval", interval: "unknown", expectedFormat: "%Y-%m"},
		{name: "case insensitive interval", interval: "DAY", expectedFormat: "%Y-%m-%d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := &HistogramAggregation{
				Field:    "created_at",
				Interval: tt.interval,
			}
			query, args := agg.SQL()

			if len(args) != 0 {
				t.Errorf("HistogramAggregation.SQL() expected 0 args, got %d", len(args))
			}

			expectedFragment := "strftime('" + tt.expectedFormat + "', d.created_at)"
			if !strings.Contains(query, expectedFragment) {
				t.Errorf("HistogramAggregation.SQL() query = %v, expected it to contain %v", query, expectedFragment)
			}
		})
	}
}

// ------------------------------------------------------------------
// TestCountAggregation_SQL (from PR #9)
// ------------------------------------------------------------------

func TestCountAggregation_SQL(t *testing.T) {
	agg := &CountAggregation{}
	gotSQL, args := agg.SQL()

	if len(args) != 0 {
		t.Errorf("Expected 0 args, got %d", len(args))
	}

	expectedSQL := "SELECT COUNT(*) FROM documents d JOIN collections c ON c.id = d.collection_id"
	if gotSQL != expectedSQL {
		t.Errorf("Expected SQL %q, but got: %q", expectedSQL, gotSQL)
	}
}

// ------------------------------------------------------------------
// TestExecuteAggregation (from PR #15)
// ------------------------------------------------------------------

func TestExecuteAggregation(t *testing.T) {
	db := setupTestDB(t)

	tests := []struct {
		name    string
		agg     Aggregation
		want    []Bucket
		wantErr bool
	}{
		{
			name: "TermAggregation collection",
			agg:  &TermAggregation{Field: "collection"},
			want: []Bucket{
				{Key: "src", Count: 3},
				{Key: "docs", Count: 2},
			},
			wantErr: false,
		},
		{
			name: "TermAggregation type",
			agg:  &TermAggregation{Field: "type"},
			want: []Bucket{
				{Key: "code", Count: 3},
				{Key: "text", Count: 2},
			},
			wantErr: false,
		},
		{
			name: "CountAggregation",
			agg:  &CountAggregation{},
			want: []Bucket{
				{Key: "count", Count: 5},
			},
			wantErr: false,
		},
		{
			name: "HistogramAggregation month",
			agg:  &HistogramAggregation{Field: "created_at", Interval: "month"},
			want: []Bucket{
				{Key: "2023-01", Count: 1},
				{Key: "2023-02", Count: 2},
				{Key: "2023-03", Count: 2},
			},
			wantErr: false,
		},
		{
			name: "RangeAggregation line_count",
			agg:  &RangeAggregation{Field: "line_count", Ranges: []string{"0-100", "100-500", "500-"}},
			want: []Bucket{
				{Key: "0-100", Count: 2},
				{Key: "100-500", Count: 2},
				{Key: "500-", Count: 1},
			},
			wantErr: false,
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
				t.Errorf("ExecuteAggregation() got length %v, want length %v. got=%+v", len(got), len(tt.want), got)
				return
			}

			for i := range got {
				if got[i].Key != tt.want[i].Key || got[i].Count != tt.want[i].Count {
					t.Errorf("ExecuteAggregation() bucket %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ------------------------------------------------------------------
// TestExecuteAggregationQueryError (from PR #15)
// ------------------------------------------------------------------

func TestExecuteAggregationQueryError(t *testing.T) {
	db := setupTestDB(t)
	// Drop tables to force a query error.
	if _, err := db.Exec("DROP TABLE documents"); err != nil {
		t.Fatalf("failed to drop table: %v", err)
	}

	agg := &CountAggregation{}
	_, err := ExecuteAggregation(db, agg)
	if err == nil {
		t.Errorf("ExecuteAggregation() expected error, got nil")
	}
}

// ------------------------------------------------------------------
// TestExecuteAggregation_Error (from PR #16) + badAggregation type
// ------------------------------------------------------------------

type badAggregation struct{}

func (a *badAggregation) SQL() (string, []interface{}) {
	return "SELECT * FROM non_existent_table", nil
}
func (a *badAggregation) Scan(rows *sql.Rows) ([]Bucket, error) {
	return nil, nil
}

func TestExecuteAggregation_Error(t *testing.T) {
	db := setupTestDB(t)

	agg := &badAggregation{}
	if _, err := ExecuteAggregation(db, agg); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

// ------------------------------------------------------------------
// TestParseAggregation_Aggs (from PR #30)
// ------------------------------------------------------------------

func TestParseAggregation_Aggs(t *testing.T) {
	tests := []struct {
		name     string
		spec     string
		wantType interface{}
		wantErr  bool
	}{
		{"count", "count", &CountAggregation{}, false},
		{"terms", "type:terms", &TermAggregation{}, false},
		{"histogram_default", "created_at:histogram", &HistogramAggregation{}, false},
		{"histogram_with_interval", "created_at:histogram:year", &HistogramAggregation{}, false},
		{"range_default", "line_count:range", &RangeAggregation{}, false},
		{"range_custom", "line_count:range:0-10,10-", &RangeAggregation{}, false},

		{"invalid_spec", "invalid", nil, true},
		{"unknown_type", "field:unknown", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAggregation(tt.spec)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAggregation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				switch want := tt.wantType.(type) {
				case *CountAggregation:
					if _, ok := got.(*CountAggregation); !ok {
						t.Errorf("ParseAggregation() got %T, want %T", got, want)
					}
				case *TermAggregation:
					if g, ok := got.(*TermAggregation); !ok {
						t.Errorf("ParseAggregation() got %T, want %T", got, want)
					} else {
						expectedField := strings.SplitN(tt.spec, ":", 2)[0]
						if g.Field != expectedField {
							t.Errorf("Expected field %q, got %q", expectedField, g.Field)
						}
					}
				case *HistogramAggregation:
					if g, ok := got.(*HistogramAggregation); !ok {
						t.Errorf("ParseAggregation() got %T, want %T", got, want)
					} else {
						parts := strings.SplitN(tt.spec, ":", 3)
						expectedField := parts[0]
						expectedInterval := "month" // default
						if len(parts) == 3 {
							expectedInterval = parts[2]
						}

						if g.Field != expectedField {
							t.Errorf("Expected field %q, got %q", expectedField, g.Field)
						}
						if g.Interval != expectedInterval {
							t.Errorf("Expected interval %q, got %q", expectedInterval, g.Interval)
						}
					}
				case *RangeAggregation:
					if g, ok := got.(*RangeAggregation); !ok {
						t.Errorf("ParseAggregation() got %T, want %T", got, want)
					} else {
						parts := strings.SplitN(tt.spec, ":", 3)
						expectedField := parts[0]
						var expectedRanges []string
						if len(parts) == 3 && parts[2] != "" {
							expectedRanges = strings.Split(parts[2], ",")
						} else {
							expectedRanges = []string{"0-100", "100-500", "500-"} // default
						}

						if g.Field != expectedField {
							t.Errorf("Expected field %q, got %q", expectedField, g.Field)
						}

						if len(g.Ranges) != len(expectedRanges) {
							t.Errorf("Expected %d ranges, got %d", len(expectedRanges), len(g.Ranges))
						} else {
							for i, r := range expectedRanges {
								if g.Ranges[i] != r {
									t.Errorf("Expected range at index %d to be %q, got %q", i, r, g.Ranges[i])
								}
							}
						}
					}
				}
			}
		})
	}
}
