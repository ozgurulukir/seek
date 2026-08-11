package search

import (
	"database/sql"
	"fmt"
	"strings"
)

// AggregationType represents the type of aggregation.
type AggregationType string

const (
	AggregationTerm      AggregationType = "terms"
	AggregationHistogram AggregationType = "histogram"
	AggregationRange     AggregationType = "range"
	AggregationCount     AggregationType = "count"
)

// Aggregation represents a search aggregation.
type Aggregation interface {
	// SQL returns the SQL query fragment for this aggregation.
	SQL() (query string, args []interface{})
	// Scan scans the aggregation result from a row.
	Scan(rows *sql.Rows) ([]Bucket, error)
}

// Bucket represents an aggregation bucket.
type Bucket struct {
	Key   string
	Count int
}

// --- Aggregation Types ---

// escapeColumnName escapes a column identifier to prevent SQL injection.
// It produces delimited (double-quoted) identifiers.
// It handles compound identifiers (e.g. c.name) by escaping each part individually.
func escapeColumnName(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		// Wrap in double quotes and escape existing double quotes
		parts[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}

// TermAggregation counts documents by a field value.
type TermAggregation struct {
	Field string
}

func (a *TermAggregation) SQL() (string, []interface{}) {
	field := a.Field
	// Map common field names to actual columns
	switch strings.ToLower(field) {
	case "type", "doc_type":
		field = "c.type"
	case "collection":
		field = "c.name"
	case "created_at", "date":
		field = "d.created_at"
	case "line_count":
		field = "d.line_count"
	case "path":
		field = "d.path"
	default:
		// Assume it's a collection field
		if !strings.Contains(field, ".") {
			field = "c." + field
		}
	}
	escapedField := escapeColumnName(field)
	return fmt.Sprintf("SELECT %s as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY %s ORDER BY count DESC", escapedField, escapedField), nil
}

func (a *TermAggregation) Scan(rows *sql.Rows) ([]Bucket, error) {
	var buckets []Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Key, &b.Count); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, nil
}

// HistogramAggregation counts documents in time-based buckets.
type HistogramAggregation struct {
	Field    string
	Interval string // "day", "week", "month", "year"
}

func (a *HistogramAggregation) SQL() (string, []interface{}) {
	field := "d.created_at"
	var format string
	switch strings.ToLower(a.Interval) {
	case "day":
		format = "%Y-%m-%d"
	case "week":
		format = "%Y-%W"
	case "month":
		format = "%Y-%m"
	case "year":
		format = "%Y"
	default:
		format = "%Y-%m"
	}
	return fmt.Sprintf("SELECT strftime('%s', %s) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key", format, field), nil
}

func (a *HistogramAggregation) Scan(rows *sql.Rows) ([]Bucket, error) {
	var buckets []Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Key, &b.Count); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, nil
}

// RangeAggregation counts documents in numeric ranges.
type RangeAggregation struct {
	Field  string
	Ranges []string // "0-100", "100-500", "500-"
}

func (a *RangeAggregation) SQL() (string, []interface{}) {
	field := "d.line_count"
	var cases []string
	var args []interface{}
	for _, r := range a.Ranges {
		parts := strings.SplitN(r, "-", 2)
		if len(parts) == 2 {
			low, high := parts[0], parts[1]
			if high == "" {
				cases = append(cases, fmt.Sprintf("WHEN %s >= ? THEN ?", field))
				args = append(args, low, r)
			} else {
				cases = append(cases, fmt.Sprintf("WHEN %s >= ? AND %s < ? THEN ?", field, field))
				args = append(args, low, high, r)
			}
		}
	}
	caseSQL := strings.Join(cases, " ")
	return fmt.Sprintf("SELECT CASE %s ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key", caseSQL), args
}

func (a *RangeAggregation) Scan(rows *sql.Rows) ([]Bucket, error) {
	var buckets []Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Key, &b.Count); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, nil
}

// CountAggregation returns the total count of matching documents.
type CountAggregation struct{}

func (a *CountAggregation) SQL() (string, []interface{}) {
	return "SELECT COUNT(*) FROM documents d JOIN collections c ON c.id = d.collection_id", nil
}

func (a *CountAggregation) Scan(rows *sql.Rows) ([]Bucket, error) {
	var b Bucket
	for rows.Next() {
		if err := rows.Scan(&b.Count); err != nil {
			return nil, err
		}
		b.Key = "count"
	}
	return []Bucket{b}, nil
}

// --- Aggregation Parsing ---

// ParseAggregation parses an aggregation spec string like "type:terms" or "created_at:histogram:month".
func ParseAggregation(spec string) (Aggregation, error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) < 2 && spec != "count" {
		return nil, fmt.Errorf("invalid aggregation spec %q: expected field:type[:interval]", spec)
	}
	if len(parts) == 1 && spec == "count" {
		return &CountAggregation{}, nil
	}
	field := parts[0]
	aggType := strings.ToLower(parts[1])

	switch aggType {
	case "terms":
		return &TermAggregation{Field: field}, nil
	case "histogram":
		interval := "month"
		if len(parts) == 3 {
			interval = parts[2]
		}
		return &HistogramAggregation{Field: field, Interval: interval}, nil
	case "range":
		ranges := []string{"0-100", "100-500", "500-"}
		if len(parts) == 3 && parts[2] != "" {
			ranges = strings.Split(parts[2], ",")
		}
		return &RangeAggregation{Field: field, Ranges: ranges}, nil
	case "count":
		return &CountAggregation{}, nil
	default:
		return nil, fmt.Errorf("unknown aggregation type %q", aggType)
	}
}

// ExecuteAggregation runs an aggregation query against the store.
func ExecuteAggregation(db *sql.DB, agg Aggregation) ([]Bucket, error) {
	query, args := agg.SQL()
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregation query: %w", err)
	}
	defer rows.Close()
	return agg.Scan(rows)
}
