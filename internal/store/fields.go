package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// FastFieldSummary provides discovery statistics for a single fast field.
type FastFieldSummary struct {
	FieldName      string             `json:"field_name"`
	MatchMode      string             `json:"match_mode"` // "exact" or "membership"
	Mode           FastFieldMatchMode `json:"-"`
	DistinctValues int                `json:"distinct_values"`
	DocCount       int                `json:"doc_count"`
	TotalDocs      int                `json:"total_docs"`
}

// FieldValueCount records a single distinct fast field value and the number of documents
// containing it.
type FieldValueCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// ListFastFieldOptions configures listing of fast field values.
type ListFastFieldOptions struct {
	Collection string // optional collection name filter
	Prefix     string // optional case-insensitive prefix match
	Limit      int    // max items to return (0 or >0; 0 defaults to 50; -1 means unlimited)
}

// GetFastFieldSummary retrieves summary statistics across all supported fast fields.
func (s *Store) GetFastFieldSummary(collection string) ([]FastFieldSummary, error) {
	return s.GetFastFieldSummaryContext(context.Background(), collection)
}

// GetFastFieldSummaryContext retrieves summary statistics across all supported fast fields with context.
func (s *Store) GetFastFieldSummaryContext(ctx context.Context, collection string) ([]FastFieldSummary, error) {
	if err := s.FastFields().ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure fast fields table: %w", err)
	}

	var totalDocs int
	var err error
	if collection != "" {
		err = s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM documents d
			 JOIN collections c ON c.id = d.collection_id
			 WHERE c.name = ?`, collection,
		).Scan(&totalDocs)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&totalDocs)
	}
	if err != nil {
		return nil, fmt.Errorf("count total documents: %w", err)
	}

	var aggQuery strings.Builder
	var aggArgs []interface{}
	aggQuery.WriteString(`SELECT ff.field_name, COUNT(DISTINCT ff.field_value), COUNT(DISTINCT ff.doc_id)
		FROM fast_fields ff
		JOIN documents d ON d.id = ff.doc_id`)
	if collection != "" {
		aggQuery.WriteString(` JOIN collections c ON c.id = d.collection_id WHERE c.name = ? AND `)
		aggArgs = append(aggArgs, collection)
	} else {
		aggQuery.WriteString(` WHERE `)
	}
	aggQuery.WriteString(`ff.field_value IS NOT NULL AND ff.field_value != '' AND ff.field_value != '""' GROUP BY ff.field_name`)

	rows, err := s.db.QueryContext(ctx, aggQuery.String(), aggArgs...)
	if err != nil {
		return nil, fmt.Errorf("query fast field aggregates: %w", err)
	}
	defer rows.Close()

	type rawStat struct {
		distinctRaw int
		docCount    int
	}
	stats := make(map[string]rawStat)
	for rows.Next() {
		var name string
		var stat rawStat
		if err := rows.Scan(&name, &stat.distinctRaw, &stat.docCount); err != nil {
			return nil, fmt.Errorf("scan fast field aggregate: %w", err)
		}
		stats[name] = stat
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fast field aggregates: %w", err)
	}

	fields := SupportedFastFields()
	summaries := make([]FastFieldSummary, 0, len(fields))

	for _, field := range fields {
		mode, _ := FieldMatchMode(field)
		modeStr := "exact"
		if mode == FastFieldMembership {
			modeStr = "membership"
		}

		st := stats[field]
		distinctCount := st.distinctRaw

		if mode == FastFieldMembership && st.docCount > 0 {
			var memQuery strings.Builder
			var memArgs []interface{}
			memQuery.WriteString(`SELECT DISTINCT REPLACE(ff.field_value, '"', '')
				FROM fast_fields ff
				JOIN documents d ON d.id = ff.doc_id`)
			if collection != "" {
				memQuery.WriteString(` JOIN collections c ON c.id = d.collection_id WHERE c.name = ? AND ff.field_name = ?`)
				memArgs = append(memArgs, collection, field)
			} else {
				memQuery.WriteString(` WHERE ff.field_name = ?`)
				memArgs = append(memArgs, field)
			}
			memQuery.WriteString(` AND ff.field_value IS NOT NULL AND ff.field_value != '' AND ff.field_value != '""'`)

			memRows, err := s.db.QueryContext(ctx, memQuery.String(), memArgs...)
			if err == nil {
				tokenSet := make(map[string]struct{})
				for memRows.Next() {
					var raw string
					if err := memRows.Scan(&raw); err == nil {
						for _, p := range strings.Split(raw, ",") {
							tok := strings.TrimSpace(p)
							if tok != "" {
								tokenSet[tok] = struct{}{}
							}
						}
					}
				}
				memRows.Close()
				distinctCount = len(tokenSet)
			}
		}

		summaries = append(summaries, FastFieldSummary{
			FieldName:      field,
			MatchMode:      modeStr,
			Mode:           mode,
			DistinctValues: distinctCount,
			DocCount:       st.docCount,
			TotalDocs:      totalDocs,
		})
	}

	return summaries, nil
}

// ListFastFieldValues returns distinct values and document counts for a given fast field.
func (s *Store) ListFastFieldValues(field string, opts ListFastFieldOptions) ([]FieldValueCount, error) {
	return s.ListFastFieldValuesContext(context.Background(), field, opts)
}

// ListFastFieldValuesContext returns distinct values and document counts for a given fast field with context.
func (s *Store) ListFastFieldValuesContext(ctx context.Context, field string, opts ListFastFieldOptions) ([]FieldValueCount, error) {
	if err := s.FastFields().ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure fast fields table: %w", err)
	}

	field = strings.ToLower(strings.TrimSpace(field))
	mode, ok := FieldMatchMode(field)
	if !ok {
		return nil, fmt.Errorf("unknown fast field %q (available: %s)", field, strings.Join(SupportedFastFields(), ", "))
	}

	limit := opts.Limit
	if limit == 0 {
		limit = 50
	}

	prefix := strings.TrimSpace(opts.Prefix)
	lowerPrefix := strings.ToLower(prefix)

	if mode == FastFieldExact {
		var q strings.Builder
		var args []interface{}

		q.WriteString(`SELECT REPLACE(ff.field_value, '"', '') as val, COUNT(DISTINCT ff.doc_id) as count
			FROM fast_fields ff
			JOIN documents d ON d.id = ff.doc_id
			JOIN collections c ON c.id = d.collection_id
			WHERE ff.field_name = ?`)
		args = append(args, field)

		if opts.Collection != "" {
			q.WriteString(` AND c.name = ?`)
			args = append(args, opts.Collection)
		}

		if prefix != "" {
			escapedPrefix := strings.ReplaceAll(prefix, "\\", "\\\\")
			escapedPrefix = strings.ReplaceAll(escapedPrefix, "%", "\\%")
			escapedPrefix = strings.ReplaceAll(escapedPrefix, "_", "\\_")
			q.WriteString(` AND REPLACE(ff.field_value, '"', '') LIKE ? || '%' ESCAPE '\' COLLATE NOCASE`)
			args = append(args, escapedPrefix)
		}

		q.WriteString(` AND REPLACE(ff.field_value, '"', '') != ''
			GROUP BY val
			ORDER BY count DESC, val ASC`)

		if limit > 0 {
			q.WriteString(` LIMIT ?`)
			args = append(args, limit)
		}

		rows, err := s.db.QueryContext(ctx, q.String(), args...)
		if err != nil {
			return nil, fmt.Errorf("query exact fast field values: %w", err)
		}
		defer rows.Close()

		var results []FieldValueCount
		for rows.Next() {
			var item FieldValueCount
			if err := rows.Scan(&item.Value, &item.Count); err != nil {
				return nil, fmt.Errorf("scan exact fast field value: %w", err)
			}
			results = append(results, item)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate exact fast field values: %w", err)
		}
		return results, nil
	}

	// Membership fields (tags, topics, entities)
	var q strings.Builder
	var args []interface{}
	q.WriteString(`SELECT REPLACE(ff.field_value, '"', '')
		FROM fast_fields ff
		JOIN documents d ON d.id = ff.doc_id
		JOIN collections c ON c.id = d.collection_id
		WHERE ff.field_name = ?
		AND ff.field_value IS NOT NULL AND ff.field_value != '' AND ff.field_value != '""'`)
	args = append(args, field)

	if opts.Collection != "" {
		q.WriteString(` AND c.name = ?`)
		args = append(args, opts.Collection)
	}

	rows, err := s.db.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query membership fast field values: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan membership fast field value: %w", err)
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		seenInDoc := make(map[string]struct{})
		for _, part := range strings.Split(raw, ",") {
			token := strings.TrimSpace(part)
			if token == "" {
				continue
			}
			if prefix != "" && !strings.HasPrefix(strings.ToLower(token), lowerPrefix) {
				continue
			}
			if _, seen := seenInDoc[token]; !seen {
				seenInDoc[token] = struct{}{}
				counts[token]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate membership fast field values: %w", err)
	}

	results := make([]FieldValueCount, 0, len(counts))
	for val, count := range counts {
		results = append(results, FieldValueCount{Value: val, Count: count})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Count != results[j].Count {
			return results[i].Count > results[j].Count
		}
		return results[i].Value < results[j].Value
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}
