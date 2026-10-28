package search

import (
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
)

// recordingTarget records the Add* calls a filter set makes, so we can
// assert the correct field/value reaches the persistence layer.
type recordingTarget struct {
	fields map[string]string
}

func newRecordingTarget() *recordingTarget { return &recordingTarget{fields: map[string]string{}} }

func (t *recordingTarget) AddCollection(name string)         {}
func (t *recordingTarget) AddDocType(typ string)             {}
func (t *recordingTarget) AddLanguage(language string)       {}
func (t *recordingTarget) AddTag(tag string)                 {}
func (t *recordingTarget) AddRepository(repository string)   {}
func (t *recordingTarget) AddDateRange(after, before string) {}
func (t *recordingTarget) AddChunkType(ChunkType)            {}
func (t *recordingTarget) AddPath(pattern string)            {}
func (t *recordingTarget) AddWorkspace(workspace string)     {}
func (t *recordingTarget) AddFastField(field, value string)  { t.fields[field] = value }

// TestFastFieldFilterApplies asserts that a FastFieldFilter routes to
// AddFastField(field, value) so the persistence layer receives both the
// field name and value (not just a pre-baked SQL). This is the seam that
// lets `--field topics:concurrency` work across semantic and code fields.
func TestFastFieldFilterApplies(t *testing.T) {
	target := newRecordingTarget()
	fs := NewFilterSet()
	fs.Add(FastFieldFilter("topics", "concurrency"))
	if err := fs.Apply(target); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if target.fields["topics"] != "concurrency" {
		t.Fatalf("AddFastField not called with topics/concurrency, got %v", target.fields)
	}
}

// TestFastFieldFilterExactValue asserts that --field filters by exact fast
// field value (semantic keywords are whole tokens; a keyword is its own
// value). This complements Apply: the search-package side must pass the
// field name through, not hardcode "tags" as TagFilter does.
func TestFastFieldFilterCarriesFieldName(t *testing.T) {
	f := FastFieldFilter("entities", "ORG:OpenAI")
	if f.Kind != FilterFastField || f.Field != "entities" || f.Value != "ORG:OpenAI" {
		t.Fatalf("FastFieldFilter did not carry field/value: %+v", f)
	}
}

// TestStoreFastFieldFilterExactValue documents the semantics difference
// between TagFilter (comma-list membership) and FastFieldFilter (exact
// equality) at the store level.
func TestStoreFastFieldFilterExactValue(t *testing.T) {
	// FastFieldFilter encodes + equals; a comma-joined value must match
	// exactly. This is by design: use --tag for comma-list membership, and
	// --field for a single distinct value (language, repo, an entity).
	ff := &store.FastFieldFilter{Field: "language", Value: "en"}
	sql, args, err := ff.ToSQL()
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if sql == "" || len(args) != 2 {
		t.Fatalf("FastFieldFilter.ToSQL = %q args=%v", sql, args)
	}
	if args[0] != "language" {
		t.Fatalf("expected field language, got %v", args[0])
	}
}

func TestFilterSetItems(t *testing.T) {
	t.Run("nil FilterSet", func(t *testing.T) {
		var fs *FilterSet
		if got := fs.Items(); got != nil {
			t.Fatalf("expected nil for nil FilterSet, got %v", got)
		}
	})

	t.Run("empty FilterSet", func(t *testing.T) {
		fs := NewFilterSet()
		got := fs.Items()
		if got == nil || len(got) != 0 {
			t.Fatalf("expected empty non-nil slice, got %v", got)
		}
	})

	t.Run("populated FilterSet and immutability", func(t *testing.T) {
		fs := NewFilterSet()
		f1 := CollectionFilter("docs")
		f2 := LanguageFilter("go")
		fs.Add(f1)
		fs.Add(f2)

		items := fs.Items()
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}
		if items[0] != f1 || items[1] != f2 {
			t.Fatalf("unexpected items: %v", items)
		}

		// Mutate returned slice to verify internal slice is isolated
		items[0] = CollectionFilter("modified")
		itemsAfter := fs.Items()
		if itemsAfter[0] != f1 {
			t.Fatalf("FilterSet internal state mutated: expected %v, got %v", f1, itemsAfter[0])
		}
	})
}
