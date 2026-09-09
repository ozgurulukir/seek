package store

import (
	"context"
	"fmt"
	"strings"
)

// AggregationBucket is the persistence-neutral result returned by the store
// aggregation seam. Search maps it to its own public Bucket type.
type AggregationBucket struct {
	Key   string
	Count int
}

// ExecuteAggregationContext executes a validated aggregation query inside the
// store. SQL rows and filter predicates stay below the search boundary.
// countOnly is true for the COUNT(*) shape, which returns one column instead
// of the key/count pair used by bucket aggregations.
func (s *Store) ExecuteAggregationContext(ctx context.Context, query string, args []interface{}, filters *FilterSet, countOnly bool) ([]AggregationBucket, error) {
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
		if countOnly {
			var b AggregationBucket
			if err := rows.Scan(&b.Count); err != nil {
				return nil, fmt.Errorf("scan aggregation count: %w", err)
			}
			b.Key = "count"
			buckets = append(buckets, b)
			continue
		}
		var b AggregationBucket
		if err := rows.Scan(&b.Key, &b.Count); err != nil {
			return nil, fmt.Errorf("scan aggregation bucket: %w", err)
		}
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate aggregation rows: %w", err)
	}
	return buckets, nil
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
	// Aggregations are document-level queries and do not join chunks, while
	// normal search filters express chunk type through the search join alias.
	// Translate that predicate to a document subquery at this persistence
	// boundary instead of leaking aggregation-specific SQL into search.
	clause = strings.ReplaceAll(clause, "ch.chunk_type = ?", "d.id IN (SELECT document_id FROM chunks WHERE chunk_type = ?)")
	upper := strings.ToUpper(query)
	groupAt := strings.Index(upper, " GROUP BY ")
	if groupAt < 0 {
		groupAt = len(query)
	}
	prefix, suffix := query[:groupAt], query[groupAt:]
	if strings.Contains(strings.ToUpper(prefix), " WHERE ") {
		prefix += " AND " + clause
	} else {
		prefix += " WHERE " + clause
	}
	return prefix + suffix, append(args, filterArgs...), nil
}
