package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecodeFastFieldText(t *testing.T) {
	cases := []struct {
		encoded string
		want    string
	}{
		{`"go,rust"`, "go,rust"},
		{`"a quote: \" inside"`, `a quote: " inside`}, // legacy REPLACE corrupted this
		{`42`, "42"},
		{`true`, "true"},
		{`null`, ""},
		{`not json`, "not json"}, // last-resort quote strip
	}
	for _, c := range cases {
		if got := decodeFastFieldText(c.encoded); got != c.want {
			t.Errorf("decodeFastFieldText(%q) = %q, want %q", c.encoded, got, c.want)
		}
	}
}

func TestSplitMembershipTokens(t *testing.T) {
	got := splitMembershipTokens("go, rust ,,org:OpenAI")
	want := []string{"go", "rust", "org:OpenAI"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitMembershipTokens = %v, want %v", got, want)
	}
	if got := splitMembershipTokens(""); got != nil {
		t.Errorf("splitMembershipTokens(\"\") = %v, want nil", got)
	}
}

func TestDynamicFieldDiscoveryAndFiltering(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("notes", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	for i, author := range []string{"jane", "jane", "bob"} {
		path := "/tmp/note" + string(rune('a'+i)) + ".md"
		docID, err := s.UpsertDocument(col.ID, path, "Note", "h", 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.FastFields().Set(docID, "author", author); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertFTS(docID, "Note", "note body content"); err != nil {
			t.Fatal(err)
		}
	}

	// Summary surfaces the non-curated field in exact mode alongside the
	// curated block.
	summaries, err := s.GetFastFieldSummaryContext(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	var authorSummary *FastFieldSummary
	for i := range summaries {
		if summaries[i].FieldName == "author" {
			authorSummary = &summaries[i]
		}
	}
	if authorSummary == nil {
		t.Fatal("dynamic field 'author' missing from summary")
	}
	if authorSummary.MatchMode != "exact" || authorSummary.DocCount != 3 || authorSummary.DistinctValues != 2 {
		t.Errorf("author summary = %+v, want exact/3 docs/2 distinct", authorSummary)
	}

	// Values list works in exact mode.
	values, err := s.ListFastFieldValuesContext(t.Context(), "author", ListFastFieldOptions{})
	if err != nil {
		t.Fatalf("dynamic values list: %v", err)
	}
	if len(values) != 2 || values[0].Value != "jane" || values[0].Count != 2 {
		t.Errorf("values = %+v, want jane=2 then bob", values)
	}

	// An end-to-end search filter on the dynamic field matches.
	fs := NewFilterSet()
	fs.Add(&FastFieldFilter{Field: "author", Value: "bob"})
	results, err := s.SearchFTS("Note", 10, fs)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("author:bob filter returned %d results, want 1", len(results))
	}

	// A field with no rows still errors with the discovery hint.
	if _, err := s.ListFastFieldValuesContext(t.Context(), "status", ListFastFieldOptions{}); err == nil {
		t.Fatal("expected error for field with no rows")
	} else if !strings.Contains(err.Error(), "seek fields") {
		t.Errorf("error should point at discovery: %v", err)
	}
}

func TestDynamicDiscoveryCoversSortFieldNameCollision(t *testing.T) {
	// A frontmatter key named "title" collides with the documents-column
	// sort pseudo-field; it is still a real fast field and must surface in
	// the summary and be listable.
	s := newTestStore(t)
	col, err := s.CreateCollection("notes", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := s.UpsertDocument(col.ID, "/tmp/note.md", "Note", "h", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FastFields().Set(docID, "title", "My Custom Title"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFTS(docID, "Note", "note body content"); err != nil {
		t.Fatal(err)
	}

	summaries, err := s.GetFastFieldSummaryContext(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, summary := range summaries {
		if summary.FieldName == "title" {
			found = true
			if summary.DocCount != 1 {
				t.Errorf("title summary = %+v, want 1 doc", summary)
			}
		}
	}
	if !found {
		t.Fatal("fast field 'title' missing from dynamic discovery summary")
	}

	if _, err := s.ListFastFieldValuesContext(t.Context(), "title", ListFastFieldOptions{}); err != nil {
		t.Fatalf("listing a fast field named title: %v", err)
	}
}

func TestChunkTypeFilterOnFTSPath(t *testing.T) {
	// Regression: the chunk-type filter used to emit ch.chunk_type = ?,
	// which fails on the FTS plan (no chunks alias). The subquery form must
	// work on both the FTS and vector paths.
	s := newTestStore(t)
	col, err := s.CreateCollection("notes", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := s.UpsertDocument(col.ID, "/tmp/note.md", "Note", "h", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertChunk(docID, 0, "note body content", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFTS(docID, "Note", "note body content"); err != nil {
		t.Fatal(err)
	}

	fs := NewFilterSet()
	fs.Add(&ChunkTypeFilter{Type: 0}) // text chunk exists → doc matches
	results, err := s.SearchFTS("Note", 10, fs)
	if err != nil {
		t.Fatalf("SearchFTS with chunk-type filter: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("text filter returned %d results, want 1", len(results))
	}

	fs = NewFilterSet()
	fs.Add(&ChunkTypeFilter{Type: 1}) // no image chunk → doc excluded
	results, err = s.SearchFTS("Note", 10, fs)
	if err != nil {
		t.Fatalf("SearchFTS with image filter: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("image filter returned %d results, want 0", len(results))
	}
}
