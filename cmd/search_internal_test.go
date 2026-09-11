package cmd

import (
	"testing"
)

// TestBuildFiltersFieldFlag exercises --field name:value parsing end-to-end
// through buildFilters: valid fields add a FastFieldFilter, unknown or
// malformed values return an error. A nil fieldKnown function means
// curated-only validation (the database-free path unit tests use).
func TestBuildFiltersFieldFlag(t *testing.T) {
	cases := []struct {
		name     string
		fields   []string
		wantErrs int // 0 = expect success
	}{
		{"topics", []string{"topics:concurrency"}, 0},
		{"entities with colon", []string{"entities:ORG:OpenAI"}, 0}, // value may contain ':'
		{"language", []string{"language:en"}, 0},
		{"multiple fields", []string{"topics:go", "language:en", "repo:x"}, 0},
		{"code lang", []string{"lang:rust"}, 0},
		{"unknown field", []string{"title:foo"}, 1},
		{"empty value", []string{"topics:"}, 1},
		{"no colon", []string{"topics"}, 1},
		{"empty field", []string{":bar"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &SearchCmd{Field: tc.fields}
			_, err := cmd.buildFilters(nil)
			if (err != nil) != (tc.wantErrs > 0) {
				t.Fatalf("buildFilters(%v) err = %v, want err=%v", tc.fields, err, tc.wantErrs > 0)
			}
			if tc.wantErrs == 0 && err != nil {
				t.Fatalf("unexpected error for valid fields: %v", err)
			}
		})
	}
}

func TestBuildFiltersFieldTrim(t *testing.T) {
	// field names are not trimmed; a space means "unknown field" (defense).
	cmd := &SearchCmd{Field: []string{"topics :go"}}
	if _, err := cmd.buildFilters(nil); err == nil {
		t.Fatal("expected error for whitelisted-name-with-space")
	}
}

func TestBuildFiltersFieldResolver(t *testing.T) {
	// A resolver that knows dynamically indexed names lets non-curated fields
	// through; unknown names still error. Document pseudo-fields (title)
	// are sort-only and never filterable.
	known := map[string]bool{"author": true, "parent": true}
	resolver := func(field string) bool { return known[field] }

	cmd := &SearchCmd{Field: []string{"author:jane", "parent:proj-a"}}
	filters, err := cmd.buildFilters(resolver)
	if err != nil {
		t.Fatalf("dynamic fields should be accepted: %v", err)
	}
	if filters == nil || len(filters.Items()) != 2 {
		t.Fatalf("expected 2 filters, got %v", filters)
	}

	cmd = &SearchCmd{Field: []string{"title:foo"}}
	if _, err := cmd.buildFilters(resolver); err == nil {
		t.Fatal("expected error: title is a sort-only pseudo-field, not filterable")
	}

	cmd = &SearchCmd{Field: []string{"nonexistent:x"}}
	if _, err := cmd.buildFilters(resolver); err == nil {
		t.Fatal("expected error for name unknown to both registry and resolver")
	}
}
