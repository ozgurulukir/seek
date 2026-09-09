package search

// These helpers preserve the historical aggregation unit tests while the
// production contract is persistence-neutral. SQL construction and row
// scanning below are test fixtures only; runtime aggregation is implemented
// by internal/store.

import (
	"database/sql"
	"fmt"
	"strings"
)

type legacyAggregation interface {
	SQL() (string, []interface{})
	Scan(*sql.Rows) ([]Bucket, error)
}

func legacyEscapeColumnName(name string) string {
	parts := strings.Split(name, ".")
	for i, part := range parts {
		parts[i] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}

func (a *TermAggregation) SQL() (string, []interface{}) {
	field := a.Field
	switch strings.ToLower(field) {
	case "lang", "tags", "repo", "ext", "filename", "rel_path", "workspace":
		name := strings.ToLower(field)
		return `SELECT REPLACE(ff.field_value, '"', '') as key, COUNT(*) as count
			FROM documents d JOIN collections c ON c.id = d.collection_id
			JOIN fast_fields ff ON ff.doc_id = d.id AND ff.field_name = ?
			GROUP BY ff.field_value ORDER BY count DESC`, []interface{}{name}
	}
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
		if !strings.Contains(field, ".") {
			field = "c." + field
		}
	}
	field = legacyEscapeColumnName(field)
	return fmt.Sprintf("SELECT %s as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY %s ORDER BY count DESC", field, field), nil
}

func (a *HistogramAggregation) SQL() (string, []interface{}) {
	field := a.Field
	if field == "" {
		field = "created_at"
	}
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
		if !strings.Contains(field, ".") {
			field = "c." + field
		}
	}
	field = legacyEscapeColumnName(field)
	format := "%Y-%m"
	switch strings.ToLower(a.Interval) {
	case "day":
		format = "%Y-%m-%d"
	case "week":
		format = "%Y-%W"
	case "year":
		format = "%Y"
	}
	return fmt.Sprintf("SELECT strftime(?, %s) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key", field), []interface{}{format}
}

func (a *RangeAggregation) SQL() (string, []interface{}) {
	field := "d.line_count"
	switch strings.ToLower(a.Field) {
	case "line_count", "lines", "d.line_count", "":
	case "mtime", "d.mtime":
		field = "d.mtime"
	case "id", "d.id":
		field = "d.id"
	case "collection_id", "d.collection_id":
		field = "d.collection_id"
	default:
		field = legacyEscapeColumnName(a.Field)
	}
	var cases []string
	var args []interface{}
	for _, value := range a.Ranges {
		parts := strings.SplitN(value, "-", 2)
		if len(parts) != 2 {
			continue
		}
		low, high := parts[0], parts[1]
		switch {
		case low == "" && high != "":
			cases = append(cases, fmt.Sprintf("WHEN %s < ? THEN ?", field))
			args = append(args, high, value)
		case high == "" && low != "":
			cases = append(cases, fmt.Sprintf("WHEN %s >= ? THEN ?", field))
			args = append(args, low, value)
		case low != "" && high != "":
			cases = append(cases, fmt.Sprintf("WHEN %s >= ? AND %s < ? THEN ?", field, field))
			args = append(args, low, high, value)
		}
	}
	return fmt.Sprintf("SELECT CASE %s ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key", strings.Join(cases, " ")), args
}

func (*CountAggregation) SQL() (string, []interface{}) {
	return "SELECT COUNT(*) FROM documents d JOIN collections c ON c.id = d.collection_id", nil
}

func scanLegacyBuckets(rows *sql.Rows) ([]Bucket, error) {
	var buckets []Bucket
	for rows.Next() {
		var bucket Bucket
		if err := rows.Scan(&bucket.Key, &bucket.Count); err != nil {
			return nil, err
		}
		buckets = append(buckets, bucket)
	}
	return buckets, rows.Err()
}

func (a *TermAggregation) Scan(rows *sql.Rows) ([]Bucket, error)      { return scanLegacyBuckets(rows) }
func (a *HistogramAggregation) Scan(rows *sql.Rows) ([]Bucket, error) { return scanLegacyBuckets(rows) }
func (a *RangeAggregation) Scan(rows *sql.Rows) ([]Bucket, error)     { return scanLegacyBuckets(rows) }

func (*CountAggregation) Scan(rows *sql.Rows) ([]Bucket, error) {
	var bucket Bucket
	for rows.Next() {
		if err := rows.Scan(&bucket.Count); err != nil {
			return nil, err
		}
		bucket.Key = "count"
	}
	return []Bucket{bucket}, rows.Err()
}

func ExecuteAggregation(db *sql.DB, aggregation legacyAggregation) ([]Bucket, error) {
	query, args := aggregation.SQL()
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregation query: %w", err)
	}
	defer rows.Close()
	return aggregation.Scan(rows)
}
