package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// AggregationSpec is the persistence-side form of a validated search
// aggregation. The search package owns parsing; Store owns identifiers, SQL,
// row scanning, and filter application.
type AggregationSpec struct {
	Type     string
	Field    string
	Interval string
	Ranges   []string
}

type AggregationBucket struct {
	Key   string
	Count int
}

// ExecuteAggregationContext builds and executes a whitelisted aggregation
// query entirely inside the persistence layer. No SQL or *sql.Rows crosses
// into search.
func (s *Store) ExecuteAggregationContext(ctx context.Context, spec AggregationSpec, filters *FilterSet) ([]AggregationBucket, error) {
	if strings.ToLower(spec.Type) == "terms" {
		field := strings.ToLower(strings.TrimSpace(spec.Field))
		if mode, ok := fastFieldMatchMode(field); ok {
			if mode == FastFieldMembership {
				return s.executeMembershipTermsAggregationContext(ctx, field, filters)
			}
			// Curated exact fast field.
			return s.executeFastFieldTermsAggregationContext(ctx, field, filters)
		}
		// Dynamic discovery: any field physically present in fast_fields
		// aggregates in exact mode, mirroring --field validation. Otherwise
		// fall through to the documents-column whitelists.
		if present, err := s.hasFastField(ctx, field); err != nil {
			return nil, err
		} else if present {
			return s.executeFastFieldTermsAggregationContext(ctx, field, filters)
		}
	}

	query, args, countOnly, err := buildAggregationQuery(spec)
	if err != nil {
		return nil, err
	}
	query, args, err = applyAggregationFilters(query, args, filters)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregation query: %w", err)
	}
	defer rows.Close()

	var buckets []AggregationBucket
	for rows.Next() {
		var bucket AggregationBucket
		if countOnly {
			if err := rows.Scan(&bucket.Count); err != nil {
				return nil, fmt.Errorf("scan aggregation count: %w", err)
			}
			bucket.Key = "count"
		} else if err := rows.Scan(&bucket.Key, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan aggregation bucket: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate aggregation rows: %w", err)
	}
	return buckets, nil
}

// executeFastFieldTermsAggregationContext counts whole stored values of one
// fast field (exact semantics — membership fields take the token-unnesting
// detour instead). It serves both curated and dynamically discovered fields:
// buckets group on the raw stored value (JSON encoding is injective per
// string, so raw grouping equals decoded grouping) and ride the
// (field_name, field_value) index; keys are decoded after scanning.
func (s *Store) executeFastFieldTermsAggregationContext(ctx context.Context, field string, filters *FilterSet) ([]AggregationBucket, error) {
	query := `SELECT ff.field_value as key, COUNT(*) as count
		FROM documents d
		JOIN collections c ON c.id = d.collection_id
		JOIN fast_fields ff ON ff.doc_id = d.id AND ff.field_name = ?
		GROUP BY ff.field_value ORDER BY count DESC, ff.field_value ASC`
	args := []interface{}{field}

	query, args, err := applyAggregationFilters(query, args, filters)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregation query: %w", err)
	}
	defer rows.Close()

	var buckets []AggregationBucket
	for rows.Next() {
		var bucket AggregationBucket
		if err := rows.Scan(&bucket.Key, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan aggregation bucket: %w", err)
		}
		bucket.Key = decodeFastFieldText(bucket.Key)
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate aggregation rows: %w", err)
	}
	return buckets, nil
}

func (s *Store) executeMembershipTermsAggregationContext(ctx context.Context, field string, filters *FilterSet) ([]AggregationBucket, error) {
	// Fetch raw values and decode/split in Go: the SQL-side REPLACE decode
	// corrupts values containing quotes, and the token counting happens
	// per row anyway.
	query := `SELECT ff.field_value
		FROM documents d
		JOIN collections c ON c.id = d.collection_id
		JOIN fast_fields ff ON ff.doc_id = d.id AND ff.field_name = ?`
	args := []interface{}{field}

	query, args, err := applyAggregationFilters(query, args, filters)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregation query: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan aggregation bucket: %w", err)
		}
		seenInDoc := make(map[string]struct{})
		for _, token := range splitMembershipTokens(decodeFastFieldText(raw)) {
			if _, seen := seenInDoc[token]; !seen {
				seenInDoc[token] = struct{}{}
				counts[token]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate aggregation rows: %w", err)
	}

	buckets := make([]AggregationBucket, 0, len(counts))
	for key, count := range counts {
		buckets = append(buckets, AggregationBucket{Key: key, Count: count})
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Count != buckets[j].Count {
			return buckets[i].Count > buckets[j].Count
		}
		return buckets[i].Key < buckets[j].Key
	})
	return buckets, nil
}

func buildAggregationQuery(spec AggregationSpec) (string, []interface{}, bool, error) {
	switch strings.ToLower(spec.Type) {
	case "count":
		return "SELECT COUNT(*) FROM documents d JOIN collections c ON c.id = d.collection_id", nil, true, nil
	case "terms":
		return buildTermsQuery(spec.Field)
	case "histogram":
		return buildHistogramQuery(spec.Field, spec.Interval)
	case "range":
		return buildRangeQuery(spec.Field, spec.Ranges)
	default:
		return "", nil, false, fmt.Errorf("unknown aggregation type %q", spec.Type)
	}
}

// buildTermsQuery plans terms over documents/collections columns. Fast-field
// terms never reach it: ExecuteAggregationContext routes curated and
// dynamically indexed fields to executeFastFieldTermsAggregationContext.
func buildTermsQuery(field string) (string, []interface{}, bool, error) {
	field = strings.ToLower(field)
	column, err := aggregationColumn(field, map[string]string{
		"type": "c.type", "doc_type": "c.type", "collection": "c.name",
		"created_at": "d.created_at", "date": "d.created_at", "line_count": "d.line_count", "path": "d.path",
		"pattern": "c.pattern", "parser_name": "c.parser_name", "parser_version": "c.parser_version", "backend": "c.backend",
	})
	if err != nil {
		return "", nil, false, err
	}
	return fmt.Sprintf("SELECT %s as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY %s ORDER BY count DESC", column, column), nil, false, nil
}

func buildHistogramQuery(field, interval string) (string, []interface{}, bool, error) {
	if field == "" {
		field = "created_at"
	}
	column, err := aggregationColumn(strings.ToLower(field), map[string]string{
		"type": "c.type", "doc_type": "c.type", "collection": "c.name",
		"created_at": "d.created_at", "date": "d.created_at", "line_count": "d.line_count", "path": "d.path",
	})
	if err != nil {
		return "", nil, false, err
	}
	format := "%Y-%m"
	switch strings.ToLower(interval) {
	case "day":
		format = "%Y-%m-%d"
	case "week":
		format = "%Y-%W"
	case "year":
		format = "%Y"
	case "", "month":
	default:
		format = "%Y-%m"
	}
	return fmt.Sprintf("SELECT strftime(?, %s) as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key", column), []interface{}{format}, false, nil
}

func buildRangeQuery(field string, ranges []string) (string, []interface{}, bool, error) {
	column := "d.line_count"
	if field != "" {
		var err error
		column, err = aggregationColumn(strings.ToLower(field), map[string]string{
			"line_count": "d.line_count", "lines": "d.line_count", "mtime": "d.mtime", "id": "d.id", "collection_id": "d.collection_id",
		})
		if err != nil {
			return "", nil, false, err
		}
	}
	if len(ranges) == 0 {
		ranges = []string{"0-100", "100-500", "500-"}
	}
	var cases []string
	var args []interface{}
	for _, value := range ranges {
		parts := strings.SplitN(value, "-", 2)
		if len(parts) != 2 {
			return "", nil, false, fmt.Errorf("invalid range %q", value)
		}
		low, high := parts[0], parts[1]
		switch {
		case low == "" && high != "":
			cases = append(cases, fmt.Sprintf("WHEN %s < ? THEN ?", column))
			args = append(args, high, value)
		case high == "" && low != "":
			cases = append(cases, fmt.Sprintf("WHEN %s >= ? THEN ?", column))
			args = append(args, low, value)
		case low != "" && high != "":
			cases = append(cases, fmt.Sprintf("WHEN %s >= ? AND %s < ? THEN ?", column, column))
			args = append(args, low, high, value)
		default:
			return "", nil, false, fmt.Errorf("invalid empty range %q", value)
		}
	}
	caseSQL := strings.Join(cases, " ")
	return fmt.Sprintf("SELECT CASE %s ELSE 'other' END as key, COUNT(*) as count FROM documents d JOIN collections c ON c.id = d.collection_id GROUP BY key ORDER BY key", caseSQL), args, false, nil
}

func aggregationColumn(field string, allowed map[string]string) (string, error) {
	column, ok := allowed[field]
	if !ok {
		return "", fmt.Errorf("unsupported aggregation field %q", field)
	}
	return quoteQualifiedIdentifier(column), nil
}

func quoteQualifiedIdentifier(column string) string {
	parts := strings.Split(column, ".")
	for i, part := range parts {
		parts[i] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}

func applyAggregationFilters(query string, args []interface{}, filters *FilterSet) (string, []interface{}, error) {
	if filters == nil {
		return query, args, nil
	}
	clause, filterArgs, err := filters.ToSQL()
	if err != nil {
		return "", nil, err
	}
	if clause == "" {
		return query, args, nil
	}
	upper := strings.ToUpper(query)
	// Locate GROUP BY regardless of surrounding whitespace: the multiline
	// query strings indent it with tabs, and appending the clause after an
	// ORDER BY would be a syntax error.
	groupAt := strings.Index(upper, "GROUP BY")
	if groupAt < 0 {
		groupAt = len(query)
	}
	prefix, suffix := strings.TrimRight(query[:groupAt], " \t\n"), query[groupAt:]
	if strings.Contains(strings.ToUpper(prefix), " WHERE ") {
		prefix += " AND " + clause
	} else {
		prefix += " WHERE " + clause
	}
	return prefix + " " + suffix, append(args, filterArgs...), nil
}
