package indexer

import (
	"reflect"
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
)

// TestMergeFastFieldsBothNil: no native and no semantic fields → nil, so the
// store's nil-map semantics (preserve previously stored fields on replace)
// are preserved.
func TestMergeFastFieldsBothNil(t *testing.T) {
	if got := mergeFastFields(nil, nil); got != nil {
		t.Fatalf("mergeFastFields(nil, nil) = %v, want nil", got)
	}
}

// TestMergeFastFieldsEmptyMaps: empty (non-nil) maps also yield nil.
func TestMergeFastFieldsEmptyMaps(t *testing.T) {
	if got := mergeFastFields(map[string]string{}, map[string]string{}); got != nil {
		t.Fatalf("mergeFastFields(empty, empty) = %v, want nil", got)
	}
}

// TestMergeFastFieldsNativeOnly: semantic nil → native returned unchanged.
func TestMergeFastFieldsNativeOnly(t *testing.T) {
	native := map[string]string{"lang": "go", "ext": ".go"}
	if got := mergeFastFields(native, nil); !reflect.DeepEqual(got, native) {
		t.Fatalf("mergeFastFields(native, nil) = %v, want %v", got, native)
	}
}

// TestMergeFastFieldsSemanticOnly: native nil → semantic returned unchanged.
func TestMergeFastFieldsSemanticOnly(t *testing.T) {
	semantic := map[string]string{"tags": "a,b", "language": "en"}
	if got := mergeFastFields(nil, semantic); !reflect.DeepEqual(got, semantic) {
		t.Fatalf("mergeFastFields(nil, semantic) = %v, want %v", got, semantic)
	}
}

// TestMergeFastFieldsDisjoint: no key overlap → union of both maps.
func TestMergeFastFieldsDisjoint(t *testing.T) {
	native := map[string]string{"lang": "go"}
	semantic := map[string]string{"tags": "a,b", "language": "en"}
	got := mergeFastFields(native, semantic)
	want := map[string]string{"lang": "go", "tags": "a,b", "language": "en"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeFastFields(disjoint) = %v, want %v", got, want)
	}
}

// TestMergeFastFieldsNativeWins: on a non-tags key conflict the
// user/frontmatter (native) value is preserved and the semantic value is
// dropped.
func TestMergeFastFieldsNativeWins(t *testing.T) {
	native := map[string]string{"language": "tr"}
	semantic := map[string]string{"language": "en", "topics": "go"}
	got := mergeFastFields(native, semantic)
	want := map[string]string{"language": "tr", "topics": "go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeFastFields(conflict) = %v, want %v", got, want)
	}
}

// TestMergeFastFieldsTagsMergeIntoNative: the `tags` key is the plan's merge
// exception — semantic tags are deduped and added to the native tags value
// instead of being dropped, so frontmatter tags and generated tags coexist.
func TestMergeFastFieldsTagsMergeIntoNative(t *testing.T) {
	native := map[string]string{"tags": "user-tag"}
	semantic := map[string]string{"tags": "generated-tag", "language": "en"}
	got := mergeFastFields(native, semantic)
	want := map[string]string{"tags": "user-tag,generated-tag", "language": "en"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeFastFields(tags conflict) = %v, want %v", got, want)
	}
}

// TestMergeFastFieldsTagsDedup: duplicate tags across native and semantic
// lists are removed, preserving first-seen order (native first).
func TestMergeFastFieldsTagsDedup(t *testing.T) {
	native := map[string]string{"tags": "go,web,cloud"}
	semantic := map[string]string{"tags": "web,sql,go"}
	got := mergeFastFields(native, semantic)
	want := map[string]string{"tags": "go,web,cloud,sql"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeFastFields(tags dedup) = %v, want %v", got, want)
	}
}

// TestMergeFastFieldsTagsWhitespace: tag parts are trimmed; blank parts are
// skipped. The store skips empty writes on its side, so the merged value
// must be a clean comma-joined list.
func TestMergeFastFieldsTagsWhitespace(t *testing.T) {
	native := map[string]string{"tags": "go, ,web"}
	semantic := map[string]string{"tags": "web, sql,"}
	got := mergeFastFields(native, semantic)
	want := map[string]string{"tags": "go,web,sql"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeFastFields(tags whitespace) = %v, want %v", got, want)
	}
}

// TestMergeFastFieldsNativeEmptyTagsValueMerges: an empty native tags value
// does not block semantic tags — the merge treats it as "no native tags" and
// the semantic tags fill the field (dedupe edilerek eklenebilir).
func TestMergeFastFieldsNativeEmptyValueWins(t *testing.T) {
	native := map[string]string{"tags": ""}
	semantic := map[string]string{"tags": "generated"}
	got := mergeFastFields(native, semantic)
	want := map[string]string{"tags": "generated"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeFastFields(empty native) = %v, want %v", got, want)
	}
}

// TestMergeFastFieldsLangAndLanguageDistinct: code `lang` and semantic
// `language` are distinct fields and are both kept.
func TestMergeFastFieldsLangAndLanguageDistinct(t *testing.T) {
	native := map[string]string{"lang": "go"}
	semantic := map[string]string{"language": "en"}
	got := mergeFastFields(native, semantic)
	want := map[string]string{"lang": "go", "language": "en"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeFastFields(lang+language) = %v, want %v", got, want)
	}
}

// TestSemanticEligibleMatrix: only the collection types that already enrich
// today (PDF, documents, conversations) are eligible; the rest pass native
// fields through unchanged.
func TestSemanticEligibleMatrix(t *testing.T) {
	eligible := map[store.CollectionType]bool{
		store.CollectionTypePDF:       true,
		store.CollectionTypeDocuments: true,
		store.CollectionTypeClaude:    true,
		store.CollectionTypeCodex:     true,
		store.CollectionTypeMarkdown:  false,
		store.CollectionTypeCode:      false,
		store.CollectionTypeImages:    false,
		store.CollectionTypeParser:    false,
	}
	for typ, want := range eligible {
		if got := semanticEligible(typ); got != want {
			t.Errorf("semanticEligible(%q) = %v, want %v", typ, got, want)
		}
	}
}
