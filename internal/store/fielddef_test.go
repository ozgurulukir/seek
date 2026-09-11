package store

import (
	"strings"
	"testing"
)

func TestRegistryInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range curatedFastFields {
		if seen[def.Name] {
			t.Errorf("duplicate curated field %q", def.Name)
		}
		seen[def.Name] = true
		if !def.Filterable {
			t.Errorf("curated field %q must be filterable", def.Name)
		}
		if !def.Sortable {
			t.Errorf("curated field %q must be sortable", def.Name)
		}
		if def.Source != sourceFastFields {
			t.Errorf("curated field %q must live in fast_fields", def.Name)
		}
	}
	for _, def := range documentSortFields {
		if seen[def.Name] {
			t.Errorf("document field %q collides with a curated fast field", def.Name)
		}
		seen[def.Name] = true
		if def.Filterable {
			t.Errorf("document pseudo-field %q must not be filterable (sort-only)", def.Name)
		}
		if !def.Sortable {
			t.Errorf("document pseudo-field %q must be sortable", def.Name)
		}
		if def.Source != sourceDocuments {
			t.Errorf("document pseudo-field %q must have sourceDocuments", def.Name)
		}
	}
}

func TestSupportedFastFieldsCoversParserdefVocabulary(t *testing.T) {
	// The parserdef schema vocabulary writes these names into fast_fields;
	// they must be first-class curated fields or the data is unreachable.
	for _, name := range []string{"workspace", "parent", "platform", "profile", "channel", "model"} {
		if !ValidFastField(name) {
			t.Errorf("parserdef metadata field %q is not curated", name)
		}
	}
}

func TestFastFieldMatchModeLookup(t *testing.T) {
	for _, name := range []string{"tags", "topics", "entities"} {
		mode, ok := fastFieldMatchMode(name)
		if !ok || mode != FastFieldMembership {
			t.Errorf("fastFieldMatchMode(%q) = (%v, %v), want membership", name, mode, ok)
		}
	}
	for _, name := range []string{"lang", "repo", "language", "model", "parent"} {
		mode, ok := fastFieldMatchMode(name)
		if !ok || mode != FastFieldExact {
			t.Errorf("fastFieldMatchMode(%q) = (%v, %v), want exact", name, mode, ok)
		}
	}
	if _, ok := fastFieldMatchMode("title"); ok {
		t.Error("title is sort-only and must not be match-mode-curated")
	}
}

func TestSortableField(t *testing.T) {
	for _, name := range []string{"tags", "created_at", "line_count", "mtime", "path", "title"} {
		if !SortableField(name) {
			t.Errorf("SortableField(%q) = false, want true", name)
		}
	}
	if SortableField("nonexistent") {
		t.Error("unknown names must not be sortable-registry entries")
	}
}

func TestFastFieldResolver(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("notes", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := s.UpsertDocument(col.ID, "/tmp/note.md", "Note", "h", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FastFields().Set(docID, "author", "jane"); err != nil {
		t.Fatal(err)
	}

	r, err := NewFastFieldResolver(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Known("author") {
		t.Error("dynamically written field must be Known")
	}
	if !r.Known("tags") {
		t.Error("curated field must be Known")
	}
	if r.Known("status") {
		t.Error("field never written must not be Known")
	}

	names, err := s.ListFastFieldNames(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "author" {
		t.Errorf("ListFastFieldNames = %v, want [author]", names)
	}

	// A nil resolver degrades to curated-only (unit-test path).
	var nilResolver *FastFieldResolver
	if nilResolver.Known("author") {
		t.Error("nil resolver must not accept dynamic fields")
	}
	if !nilResolver.Known("repo") {
		t.Error("nil resolver must still accept curated fields")
	}
}

func TestFieldDiscoveryHintListsCuratedFields(t *testing.T) {
	hint := FieldDiscoveryHint()
	for _, name := range SupportedFastFields() {
		if !strings.Contains(hint, name) {
			t.Errorf("hint %q does not mention curated field %q", hint, name)
		}
	}
}
