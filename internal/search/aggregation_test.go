package search

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Create tables
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

	// Insert seed data
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

func TestExecuteAggregationQueryError(t *testing.T) {
	db := setupTestDB(t)
	// Drop tables to force error
	_, err := db.Exec("DROP TABLE documents")
	if err != nil {
		t.Fatalf("failed to drop table: %v", err)
	}

	agg := &CountAggregation{}
	_, err = ExecuteAggregation(db, agg)
	if err == nil {
		t.Errorf("ExecuteAggregation() expected error, got nil")
	}
}
