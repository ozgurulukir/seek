package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SortValues returns the sort key for each given document. Document-column
// pseudo-fields (created_at, line_count, mtime, path, title) are resolved
// from the documents table; every other name is read from fast_fields and
// simply yields no entry for documents that lack the field. Values are
// string or float64, matching what the search engine's comparator handles;
// documents without a value are absent from the map (the engine sorts them
// last, preserving relative order).
func (s *Store) SortValues(ctx context.Context, docIDs []int64, field string) (map[int64]interface{}, error) {
	if def, ok := lookupFieldDef(field); ok && def.Source == sourceDocuments {
		return s.documentSortValues(ctx, docIDs, field)
	}
	return s.FastFields().BatchGetContext(ctx, docIDs, field)
}

// documentSortValues reads the requested documents column for the given IDs.
// created_at is RFC3339 TEXT, so lexicographic order is chronological;
// line_count and mtime are emitted as float64.
//
// Known scale caveat (pre-existing data property, not fixed here): mtime is
// file-mtime seconds for file-backed collections but a Unix-milli cursor for
// parserdef collections, so cross-collection --sort-by mtime compares
// incompatible scales.
func (s *Store) documentSortValues(ctx context.Context, docIDs []int64, field string) (map[int64]interface{}, error) {
	if len(docIDs) == 0 {
		return map[int64]interface{}{}, nil
	}
	// Names reach here only via the registry (SortValues gates on
	// sourceDocuments), and the column name equals the registry name, so the
	// identifier is code-owned despite being formatted in.
	column := field

	placeholders := make([]string, 0, len(docIDs))
	args := make([]interface{}, 0, len(docIDs))
	for _, id := range docIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf(`SELECT id, %s FROM documents WHERE id IN (%s)`, column, strings.Join(placeholders, ","))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sort values query: %w", err)
	}
	defer rows.Close()

	values := make(map[int64]interface{}, len(docIDs))
	switch field {
	case "line_count", "mtime":
		for rows.Next() {
			var id int64
			var num sql.NullFloat64
			if err := rows.Scan(&id, &num); err != nil {
				return nil, fmt.Errorf("scan sort values: %w", err)
			}
			if num.Valid {
				values[id] = num.Float64
			}
		}
	default:
		for rows.Next() {
			var id int64
			var text sql.NullString
			if err := rows.Scan(&id, &text); err != nil {
				return nil, fmt.Errorf("scan sort values: %w", err)
			}
			if text.Valid && text.String != "" {
				values[id] = text.String
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sort values: %w", err)
	}
	return values, nil
}
