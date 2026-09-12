package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// FastFieldSummary provides discovery statistics for a single fast field.
type FastFieldSummary struct {
	FieldName      string `json:"field_name"`
	MatchMode      string `json:"match_mode"` // "exact" or "membership"
	DistinctValues int    `json:"distinct_values"`
	DocCount       int    `json:"doc_count"`
	TotalDocs      int    `json:"total_docs"`
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
	// Dynamic discovery: fast_fields may hold field names with no curated
	// registry entry (arbitrary markdown frontmatter keys, future parsers).
	// The aggregate above already grouped by every present field name, so
	// surfacing the extras costs no additional SQL. They are exact-mode by
	// definition and sort alphabetically after the curated block.
	extras := make([]string, 0, len(stats))
	for name := range stats {
		// Gate on curated-filterable, not on registry presence: a frontmatter
		// key that happens to collide with a documents-column sort field
		// (title, path, created_at, ...) is still a real fast field and must
		// be discovered.
		if _, curated := fastFieldMatchMode(name); !curated {
			extras = append(extras, name)
		}
	}
	sort.Strings(extras)
	fields = append(fields, extras...)
	summaries := make([]FastFieldSummary, 0, len(fields))

	for _, field := range fields {
		// fastFieldMatchMode already returns exact for non-curated names.
		mode, _ := fastFieldMatchMode(field)
		modeStr := "exact"
		if mode == FastFieldMembership {
			modeStr = "membership"
		}

		st := stats[field]
		distinctCount := st.distinctRaw

		if mode == FastFieldMembership && st.docCount > 0 {
			memQuery := `SELECT ff.field_value
				FROM fast_fields ff
				JOIN documents d ON d.id = ff.doc_id`
			var memArgs []interface{}
			if collection != "" {
				memQuery += ` JOIN collections c ON c.id = d.collection_id WHERE c.name = ? AND ff.field_name = ?`
				memArgs = append(memArgs, collection, field)
			} else {
				memQuery += ` WHERE ff.field_name = ?`
				memArgs = append(memArgs, field)
			}
			memQuery += ` AND ff.field_value IS NOT NULL AND ff.field_value != '' AND ff.field_value != '""'`

			memRows, err := s.db.QueryContext(ctx, memQuery, memArgs...)
			if err == nil {
				tokenSet := make(map[string]struct{})
				for memRows.Next() {
					var raw string
					if err := memRows.Scan(&raw); err == nil {
						for _, p := range splitMembershipTokens(decodeFastFieldText(raw)) {
							tokenSet[p] = struct{}{}
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

// ListFastFieldValuesContext returns distinct values and document counts for a given fast field.
func (s *Store) ListFastFieldValuesContext(ctx context.Context, field string, opts ListFastFieldOptions) ([]FieldValueCount, error) {
	if err := s.FastFields().ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure fast fields table: %w", err)
	}

	field = strings.ToLower(strings.TrimSpace(field))
	mode, curated := fastFieldMatchMode(field)
	if !curated {
		// Dynamic discovery: any field physically present in fast_fields is
		// listable, in exact mode. seek fields inspects one field per
		// invocation, so the single probe stays cheap.
		present, err := s.hasFastField(ctx, field)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, fmt.Errorf("unknown fast field %q (%s)", field, FieldDiscoveryHint())
		}
		// fastFieldMatchMode already returned FastFieldExact for the miss.
	}

	limit := opts.Limit
	if limit == 0 {
		limit = 50
	}

	prefix := strings.TrimSpace(opts.Prefix)
	lowerPrefix := strings.ToLower(prefix)

	if mode == FastFieldExact {
		// Group on the raw indexed column (no per-row SQL decoding), then
		// decode, prefix-filter, sort, and limit in Go.
		q := `SELECT ff.field_value, COUNT(DISTINCT ff.doc_id)
			FROM fast_fields ff
			JOIN documents d ON d.id = ff.doc_id
			JOIN collections c ON c.id = d.collection_id
			WHERE ff.field_name = ?`
		args := []interface{}{field}

		if opts.Collection != "" {
			q += ` AND c.name = ?`
			args = append(args, opts.Collection)
		}

		q += ` AND ff.field_value IS NOT NULL AND ff.field_value != '' AND ff.field_value != '""'
			GROUP BY ff.field_value`

		rows, err := s.db.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, fmt.Errorf("query exact fast field values: %w", err)
		}
		defer rows.Close()

		var results []FieldValueCount
		for rows.Next() {
			var raw string
			var item FieldValueCount
			if err := rows.Scan(&raw, &item.Count); err != nil {
				return nil, fmt.Errorf("scan exact fast field value: %w", err)
			}
			item.Value = decodeFastFieldText(raw)
			if item.Value == "" {
				continue
			}
			if prefix != "" && !strings.HasPrefix(strings.ToLower(item.Value), lowerPrefix) {
				continue
			}
			results = append(results, item)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate exact fast field values: %w", err)
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

	// Membership fields (tags, topics, entities)
	q := `SELECT ff.field_value
		FROM fast_fields ff
		JOIN documents d ON d.id = ff.doc_id
		JOIN collections c ON c.id = d.collection_id
		WHERE ff.field_name = ?
		AND ff.field_value IS NOT NULL AND ff.field_value != '' AND ff.field_value != '""'`
	args := []interface{}{field}

	if opts.Collection != "" {
		q += ` AND c.name = ?`
		args = append(args, opts.Collection)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
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

		seenInDoc := make(map[string]struct{})
		for _, token := range splitMembershipTokens(decodeFastFieldText(raw)) {
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

// splitMembershipTokens splits a decoded comma-joined multi-value into its
// trimmed, non-empty tokens, preserving first-seen order.
func splitMembershipTokens(decoded string) []string {
	var tokens []string
	for _, part := range strings.Split(decoded, ",") {
		token := strings.TrimSpace(part)
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

// hasFastField reports whether the field name has at least one stored value
// in fast_fields. The probe rides the field_name prefix of
// idx_fast_fields_name_value; it is the dynamic-discovery check shared by
// the field listing and the terms aggregation path. The table is created
// lazily on first write, so its absence simply means no dynamic fields exist.
func (s *Store) hasFastField(ctx context.Context, field string) (bool, error) {
	var present bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM fast_fields WHERE field_name = ?)`, field,
	).Scan(&present); err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return false, nil
		}
		return false, fmt.Errorf("resolve fast field: %w", err)
	}
	return present, nil
}
