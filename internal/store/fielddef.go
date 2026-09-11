package store

import (
	"context"
	"fmt"
	"strings"
)

// FastFieldMatchMode controls how a FastFieldFilter matches a value.
type FastFieldMatchMode int

const (
	// FastFieldExact matches the whole value exactly (single-value fields).
	FastFieldExact FastFieldMatchMode = iota
	// FastFieldMembership matches membership in a comma-separated list
	// (multi-value fields: tags, topics, entities).
	FastFieldMembership
)

// fieldSource says where a field's values physically live: one fast_fields
// row per document, or a documents table column (sortable pseudo-fields).
type fieldSource uint8

const (
	sourceFastFields fieldSource = iota
	sourceDocuments
)

// fieldDef is one entry of the fast-field registry. The registry is the
// single source of truth for which field names exist, how they match
// (exact vs membership), where their values live, and what they may be used
// for (filtering, sorting). Writers remain schemaless (DocumentIndex.FastFields
// is a plain map); the registry is a read-side contract that consumers derive
// their whitelists from instead of keeping their own.
type fieldDef struct {
	Name       string
	Mode       FastFieldMatchMode
	Source     fieldSource
	Filterable bool // may appear in FastFieldFilter / --field / seek fields
	Sortable   bool // may appear in Options.SortBy / --sort-by
}

// curatedFastFields is the code-owned registry in canonical display order
// (the order seek fields prints). Adding a field = one entry here; every
// consumer (filters, discovery, CLI validation, help text) derives from it.
var curatedFastFields = []fieldDef{
	{"tags", FastFieldMembership, sourceFastFields, true, true},
	{"topics", FastFieldMembership, sourceFastFields, true, true},
	{"entities", FastFieldMembership, sourceFastFields, true, true},
	{"language", FastFieldExact, sourceFastFields, true, true},
	{"lang", FastFieldExact, sourceFastFields, true, true},
	{"ext", FastFieldExact, sourceFastFields, true, true},
	{"filename", FastFieldExact, sourceFastFields, true, true},
	{"rel_path", FastFieldExact, sourceFastFields, true, true},
	{"repo", FastFieldExact, sourceFastFields, true, true},
	{"workspace", FastFieldExact, sourceFastFields, true, true},
	// Parserdef conversation context (parsers/*.yaml session metadata). The
	// parserdef schema vocabulary (knownMetadataFields) must stay a subset of
	// this list; internal/source/parserdef/registry_sync_test.go asserts it.
	{"parent", FastFieldExact, sourceFastFields, true, true},
	{"platform", FastFieldExact, sourceFastFields, true, true},
	{"profile", FastFieldExact, sourceFastFields, true, true},
	{"channel", FastFieldExact, sourceFastFields, true, true},
	{"model", FastFieldExact, sourceFastFields, true, true},
}

// documentSortFields are sortable pseudo-fields resolved from documents
// table columns instead of fast_fields (see sortvalues.go). They are
// deliberately not filterable: "title:foo" in --field must keep being
// rejected, only --sort-by reaches them.
var documentSortFields = []fieldDef{
	{"created_at", FastFieldExact, sourceDocuments, false, true},
	{"line_count", FastFieldExact, sourceDocuments, false, true},
	{"mtime", FastFieldExact, sourceDocuments, false, true},
	{"path", FastFieldExact, sourceDocuments, false, true},
	{"title", FastFieldExact, sourceDocuments, false, true},
}

// lookupFieldDef finds a registry entry across both tables. The tables are
// tiny (20 entries), so a linear scan beats maintaining a map.
func lookupFieldDef(name string) (fieldDef, bool) {
	for _, def := range curatedFastFields {
		if def.Name == name {
			return def, true
		}
	}
	for _, def := range documentSortFields {
		if def.Name == name {
			return def, true
		}
	}
	return fieldDef{}, false
}

// fastFieldMatchMode returns the match mode for a curated filterable fast
// field, and whether the field is curated-filterable at all. It is the
// single derivation every read path (filters, discovery, aggregation) uses;
// unknown names default to exact so dynamically discovered fields can be
// filtered with plain equality.
func fastFieldMatchMode(field string) (FastFieldMatchMode, bool) {
	def, ok := lookupFieldDef(field)
	if !ok || !def.Filterable || def.Source != sourceFastFields {
		return FastFieldExact, false
	}
	return def.Mode, true
}

// FieldMatchMode returns the match mode for a curated filterable fast field,
// and whether the field is curated-filterable at all.
func FieldMatchMode(field string) (FastFieldMatchMode, bool) {
	return fastFieldMatchMode(field)
}

// SupportedFastFields returns all curated fast field names in canonical
// display order.
func SupportedFastFields() []string {
	names := make([]string, 0, len(curatedFastFields))
	for _, def := range curatedFastFields {
		names = append(names, def.Name)
	}
	return names
}

// ValidFastField reports whether field is a curated fast-field name that may
// be filtered and aggregated on. Kept central so aggregation, the --field
// filter, and FastFieldFilter share the same whitelist. Dynamically indexed
// field names (present in fast_fields but not curated) are validated by the
// FastFieldResolver instead.
func ValidFastField(field string) bool {
	_, ok := fastFieldMatchMode(field)
	return ok
}

// SortableField reports whether field may be used as --sort-by: a curated
// fast field or a documents-column pseudo-field.
func SortableField(field string) bool {
	def, ok := lookupFieldDef(field)
	return ok && def.Sortable
}

// ListFastFieldNames returns every field_name physically present in
// fast_fields, sorted. This is the discovery side of the registry: fields
// written by producers without a curated entry (markdown frontmatter keys,
// future parsers) surface here and in the field summary.
func (s *Store) ListFastFieldNames(ctx context.Context) ([]string, error) {
	if err := s.FastFields().ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure fast fields table: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT field_name FROM fast_fields ORDER BY field_name`)
	if err != nil {
		return nil, fmt.Errorf("list fast field names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan fast field name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fast field names: %w", err)
	}
	return names, nil
}

// FastFieldResolver resolves field names against the curated registry plus
// the field names physically present in fast_fields. Construct one per
// command invocation (one DISTINCT query) and check names in memory.
// A nil resolver falls back to curated-only validation, which keeps unit
// tests deterministic without a database.
type FastFieldResolver struct {
	present map[string]struct{}
}

// NewFastFieldResolver snapshots the field names currently present in
// fast_fields. A field appearing after the snapshot is simply not accepted
// by Known until the next invocation.
func NewFastFieldResolver(ctx context.Context, s *Store) (*FastFieldResolver, error) {
	names, err := s.ListFastFieldNames(ctx)
	if err != nil {
		return nil, err
	}
	r := &FastFieldResolver{present: make(map[string]struct{}, len(names))}
	for _, name := range names {
		r.present[name] = struct{}{}
	}
	return r, nil
}

// Known reports whether field may be used as a --field filter value: either
// a curated fast field or one physically present in the index. Names are
// always bound parameters downstream, so accepting an arbitrary stored name
// is SQL-safe.
func (r *FastFieldResolver) Known(field string) bool {
	if _, ok := fastFieldMatchMode(field); ok {
		return true
	}
	if r == nil {
		return false
	}
	_, ok := r.present[field]
	return ok
}

// FieldDiscoveryHint is the shared suffix of every unknown-field error so
// CLI and store errors point users at the same discovery command.
func FieldDiscoveryHint() string {
	return "run 'seek fields' to list indexed fields (curated: " +
		strings.Join(SupportedFastFields(), ", ") + ")"
}
