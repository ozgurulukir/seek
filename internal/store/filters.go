package store

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Search filter types. Extracted from store.go as part of the god-object
// decomposition — a mechanical move, no changes.

// Filter is a search filter that can be converted to a SQL WHERE clause.
type Filter interface {
	ToSQL() (clause string, args []interface{}, err error)
}

// FilterSet combines multiple filters with AND. ToSQL surfaces the first
// filter error instead of silently dropping invalid predicates from the
// query (which would widen the result set).
type FilterSet struct {
	filters []Filter
}

func NewFilterSet() *FilterSet {
	return &FilterSet{filters: make([]Filter, 0)}
}

func (fs *FilterSet) Add(f Filter) {
	fs.filters = append(fs.filters, f)
}

func (fs *FilterSet) ToSQL() (string, []interface{}, error) {
	var clauses []string
	var args []interface{}
	for _, f := range fs.filters {
		c, a, err := f.ToSQL()
		if err != nil {
			return "", nil, err
		}
		if c != "" {
			clauses = append(clauses, c)
			args = append(args, a...)
		}
	}
	return strings.Join(clauses, " AND "), args, nil
}

// --- Filter Types ---

// TagFilter matches a single tag inside a comma-separated fast-field value
// (e.g. the markdown frontmatter `tags` field, where "go,rust" must match a
// search for "go"). FastFieldFilter only does exact equality, which cannot
// express membership in a list.
type TagFilter struct {
	Tag string
}

func (f *TagFilter) ToSQL() (string, []interface{}, error) {
	// Fast-field values are JSON-encoded on write ("go,rust" is stored as
	// `"go,rust"`), so raw LIKE patterns against the raw column would need
	// quote-aware boundaries. Simpler and exact: strip the JSON quotes in
	// SQL with REPLACE, then match the tag as a whole comma-separated token
	// (whole value, head, tail, or middle).
	return `d.id IN (SELECT doc_id FROM fast_fields WHERE field_name = 'tags' AND
		(REPLACE(field_value, '"', '') = ? OR REPLACE(field_value, '"', '') LIKE ? || ',%'
		 OR REPLACE(field_value, '"', '') LIKE '%,' || ? OR REPLACE(field_value, '"', '') LIKE '%,' || ? || ',%'))`,
		[]interface{}{f.Tag, f.Tag, f.Tag, f.Tag}, nil
}

// CollectionFilter filters by collection name.
type CollectionFilter struct {
	Name string
}

func (f *CollectionFilter) ToSQL() (string, []interface{}, error) {
	return "d.collection_id = (SELECT id FROM collections WHERE name = ?)", []interface{}{f.Name}, nil
}

// DocTypeFilter filters by collection type (markdown, claude, codex, images, pdf).
type DocTypeFilter struct {
	Type string
}

func (f *DocTypeFilter) ToSQL() (string, []interface{}, error) {
	return "c.type = ?", []interface{}{f.Type}, nil
}

// DateRangeFilter filters documents by created_at range.
type DateRangeFilter struct {
	After  string // RFC3339 or empty
	Before string // RFC3339 or empty
}

func (f *DateRangeFilter) ToSQL() (string, []interface{}, error) {
	var clauses []string
	var args []interface{}
	if f.After != "" {
		clauses = append(clauses, "d.created_at >= ?")
		args = append(args, f.After)
	}
	if f.Before != "" {
		clauses = append(clauses, "d.created_at <= ?")
		args = append(args, f.Before)
	}
	return strings.Join(clauses, " AND "), args, nil
}

// ChunkTypeFilter filters chunks by chunk_type (0=text, 1=image).
type ChunkTypeFilter struct {
	Type int // 0=text, 1=image
}

func (f *ChunkTypeFilter) ToSQL() (string, []interface{}, error) {
	return "ch.chunk_type = ?", []interface{}{f.Type}, nil
}

// PathFilter filters documents by path pattern (GLOB).
type PathFilter struct {
	Pattern string
}

func (f *PathFilter) ToSQL() (string, []interface{}, error) {
	// Sanitize: reject path traversal
	if strings.Contains(f.Pattern, "..") {
		return "", nil, fmt.Errorf("path filter: pattern %q contains '..' (path traversal)", f.Pattern)
	}
	cleaned := filepath.ToSlash(filepath.Clean(f.Pattern))
	return "replace(d.path, '\\', '/') GLOB ?", []interface{}{cleaned}, nil
}

// FastFieldMatchMode controls how a FastFieldFilter matches a value.
type FastFieldMatchMode int

const (
	// FastFieldExact matches the whole value exactly (single-value fields:
	// lang, repo, ext, filename, rel_path, workspace, language).
	FastFieldExact FastFieldMatchMode = iota
	// FastFieldMembership matches membership in a comma-separated list
	// (multi-value fields: tags, topics, entities).
	FastFieldMembership
)

// fastFieldMatchMode returns the match mode for a known fast field, and
// whether the field is known at all. Single source of truth for filtering
// semantics, shared by FastFieldFilter and the ValidFastField whitelist.
func fastFieldMatchMode(field string) (FastFieldMatchMode, bool) {
	switch field {
	case "tags", "topics", "entities":
		return FastFieldMembership, true
	case "lang", "ext", "filename", "rel_path", "repo", "workspace", "language":
		return FastFieldExact, true
	default:
		return FastFieldExact, false
	}
}

// FieldMatchMode returns the match mode for a known fast field, and
// whether the field is known at all.
func FieldMatchMode(field string) (FastFieldMatchMode, bool) {
	return fastFieldMatchMode(field)
}

// SupportedFastFields returns all known fast field names in canonical display order.
func SupportedFastFields() []string {
	return []string{
		"tags", "topics", "entities",
		"language", "lang", "ext", "filename", "rel_path", "repo", "workspace",
	}
}

// ValidFastField reports whether field is a known fast-field name that may
// be filtered and aggregated on. Kept central so aggregation, the --field
// filter, and FastFieldFilter share the same whitelist.
func ValidFastField(field string) bool {
	_, ok := fastFieldMatchMode(field)
	return ok
}

// FastFieldFilter filters documents by a fast-field value (e.g. workspace).
// Uses the fast_fields table for indexed lookups.
type FastFieldFilter struct {
	Field string // fast field name (e.g. "topics")
	Value string // fast field value (single value, or a comma-list member)
}

// ToSQL selects the match strategy from the field's type: exact equality
// for single-value fields, comma-list membership for multi-value fields.
// The field name is always a bound parameter and the mode is derived from a
// fixed table (never user input), so it cannot inject SQL.
func (f *FastFieldFilter) ToSQL() (string, []interface{}, error) {
	mode, ok := fastFieldMatchMode(f.Field)
	if !ok {
		return "", nil, fmt.Errorf("fast field filter: unknown field %q", f.Field)
	}
	encoded, err := encodeFastFieldValue(f.Value)
	if err != nil {
		return "", nil, fmt.Errorf("fast field filter %q: %w", f.Field, err)
	}
	if mode == FastFieldMembership {
		// Values are JSON-encoded on write (e.g. `"go,rust"`); strip the
		// quotes and match the value as a whole comma-separated token (head,
		// tail, or middle). Same token semantics as TagFilter but on any
		// multi-value fast field. The value is a bound parameter.
		return `d.id IN (SELECT doc_id FROM fast_fields WHERE field_name = ? AND
			(REPLACE(field_value, '"', '') = ? OR REPLACE(field_value, '"', '') LIKE ? || ',%'
			 OR REPLACE(field_value, '"', '') LIKE '%,' || ? OR REPLACE(field_value, '"', '') LIKE '%,' || ? || ',%'))`,
			[]interface{}{f.Field, f.Value, f.Value, f.Value, f.Value}, nil
	}
	return "d.id IN (SELECT doc_id FROM fast_fields WHERE field_name = ? AND field_value = ?)",
		[]interface{}{f.Field, encoded}, nil
}
