package store

import (
	"context"
	"strings"
	"testing"
)

func TestFastFieldSummaryAndListValues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	col1, err := s.CreateCollection("notes", CollectionTypeMarkdown, "/notes", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection notes: %v", err)
	}
	col2, err := s.CreateCollection("code", CollectionTypeCode, "/code", "**/*.go")
	if err != nil {
		t.Fatalf("CreateCollection code: %v", err)
	}

	// Document 1 (col1): tags="golang,concurrency", language="en", workspace="ws1"
	d1, err := s.UpsertDocument(col1.ID, "/notes/d1.md", "d1.md", "h1", 1, 100)
	if err != nil {
		t.Fatalf("UpsertDocument d1: %v", err)
	}
	_ = s.FastFields().Set(d1, "tags", "golang,concurrency")
	_ = s.FastFields().Set(d1, "language", "en")
	_ = s.FastFields().Set(d1, "workspace", "ws1")

	// Document 2 (col1): tags="golang,database,sqlite", language="en", workspace="ws1"
	d2, err := s.UpsertDocument(col1.ID, "/notes/d2.md", "d2.md", "h2", 1, 120)
	if err != nil {
		t.Fatalf("UpsertDocument d2: %v", err)
	}
	_ = s.FastFields().Set(d2, "tags", "golang,database,sqlite")
	_ = s.FastFields().Set(d2, "language", "en")
	_ = s.FastFields().Set(d2, "workspace", "ws1")

	// Document 3 (col2): tags="rust,concurrency", language="tr", workspace="ws2"
	d3, err := s.UpsertDocument(col2.ID, "/code/d3.go", "d3.go", "h3", 1, 200)
	if err != nil {
		t.Fatalf("UpsertDocument d3: %v", err)
	}
	_ = s.FastFields().Set(d3, "tags", "rust,concurrency")
	_ = s.FastFields().Set(d3, "language", "tr")
	_ = s.FastFields().Set(d3, "workspace", "ws2")

	// Document 4 (col2): no fast fields
	_, err = s.UpsertDocument(col2.ID, "/code/d4.go", "d4.go", "h4", 1, 50)
	if err != nil {
		t.Fatalf("UpsertDocument d4: %v", err)
	}

	t.Run("GetFastFieldSummary all collections", func(t *testing.T) {
		summaries, err := s.GetFastFieldSummaryContext(ctx, "")
		if err != nil {
			t.Fatalf("GetFastFieldSummary: %v", err)
		}

		m := make(map[string]FastFieldSummary)
		for _, sm := range summaries {
			m[sm.FieldName] = sm
		}

		// Total docs across col1 and col2 = 4
		if m["tags"].TotalDocs != 4 {
			t.Errorf("tags.TotalDocs = %d, want 4", m["tags"].TotalDocs)
		}

		// tags: 3 documents have tags (d1, d2, d3).
		// Distinct tokens: golang, concurrency, database, sqlite, rust = 5 distinct tokens!
		if m["tags"].DocCount != 3 {
			t.Errorf("tags.DocCount = %d, want 3", m["tags"].DocCount)
		}
		if m["tags"].DistinctValues != 5 {
			t.Errorf("tags.DistinctValues = %d, want 5 (distinct tokens)", m["tags"].DistinctValues)
		}
		if m["tags"].MatchMode != "membership" {
			t.Errorf("tags.MatchMode = %s, want membership", m["tags"].MatchMode)
		}

		// language: exact field, 3 docs (d1, d2, d3), 2 distinct values ("en", "tr")
		if m["language"].DocCount != 3 {
			t.Errorf("language.DocCount = %d, want 3", m["language"].DocCount)
		}
		if m["language"].DistinctValues != 2 {
			t.Errorf("language.DistinctValues = %d, want 2", m["language"].DistinctValues)
		}
		if m["language"].MatchMode != "exact" {
			t.Errorf("language.MatchMode = %s, want exact", m["language"].MatchMode)
		}

		// empty field: entities
		if m["entities"].DocCount != 0 {
			t.Errorf("entities.DocCount = %d, want 0", m["entities"].DocCount)
		}
		if m["entities"].DistinctValues != 0 {
			t.Errorf("entities.DistinctValues = %d, want 0", m["entities"].DistinctValues)
		}
	})

	t.Run("GetFastFieldSummary scoped to notes", func(t *testing.T) {
		summaries, err := s.GetFastFieldSummaryContext(ctx, "notes")
		if err != nil {
			t.Fatalf("GetFastFieldSummary notes: %v", err)
		}
		m := make(map[string]FastFieldSummary)
		for _, sm := range summaries {
			m[sm.FieldName] = sm
		}

		if m["tags"].TotalDocs != 2 {
			t.Errorf("notes TotalDocs = %d, want 2", m["tags"].TotalDocs)
		}
		if m["tags"].DocCount != 2 {
			t.Errorf("notes tags DocCount = %d, want 2", m["tags"].DocCount)
		}
		// tokens in notes: golang, concurrency, database, sqlite = 4
		if m["tags"].DistinctValues != 4 {
			t.Errorf("notes tags DistinctValues = %d, want 4", m["tags"].DistinctValues)
		}
	})

	t.Run("ListFastFieldValues membership field (tags)", func(t *testing.T) {
		vals, err := s.ListFastFieldValuesContext(ctx, "tags", ListFastFieldOptions{})
		if err != nil {
			t.Fatalf("ListFastFieldValues tags: %v", err)
		}

		// Expected tokens and counts:
		// golang: 2 (d1, d2)
		// concurrency: 2 (d1, d3)
		// database: 1 (d2)
		// rust: 1 (d3)
		// sqlite: 1 (d2)
		if len(vals) != 5 {
			t.Fatalf("expected 5 distinct tags, got %d: %#v", len(vals), vals)
		}
		if vals[0].Value != "concurrency" && vals[0].Value != "golang" {
			t.Errorf("top value should have count 2, got %#v", vals[0])
		}
		if vals[0].Count != 2 || vals[1].Count != 2 {
			t.Errorf("expected top 2 to have count 2, got %d, %d", vals[0].Count, vals[1].Count)
		}
	})

	t.Run("ListFastFieldValues prefix filter", func(t *testing.T) {
		vals, err := s.ListFastFieldValuesContext(ctx, "tags", ListFastFieldOptions{Prefix: "go"})
		if err != nil {
			t.Fatalf("ListFastFieldValues tags prefix: %v", err)
		}
		if len(vals) != 1 || vals[0].Value != "golang" || vals[0].Count != 2 {
			t.Fatalf("expected [golang:2], got %#v", vals)
		}
	})

	t.Run("ListFastFieldValues collection filter", func(t *testing.T) {
		vals, err := s.ListFastFieldValuesContext(ctx, "tags", ListFastFieldOptions{Collection: "code"})
		if err != nil {
			t.Fatalf("ListFastFieldValues tags code collection: %v", err)
		}
		// In code (d3): concurrency (1), rust (1)
		if len(vals) != 2 {
			t.Fatalf("expected 2 tags in code, got %d: %#v", len(vals), vals)
		}
	})

	t.Run("ListFastFieldValues exact field (language)", func(t *testing.T) {
		vals, err := s.ListFastFieldValuesContext(ctx, "language", ListFastFieldOptions{})
		if err != nil {
			t.Fatalf("ListFastFieldValues language: %v", err)
		}
		// en: 2, tr: 1
		if len(vals) != 2 {
			t.Fatalf("expected 2 languages, got %d: %#v", len(vals), vals)
		}
		if vals[0].Value != "en" || vals[0].Count != 2 {
			t.Errorf("expected en:2 first, got %#v", vals[0])
		}
		if vals[1].Value != "tr" || vals[1].Count != 1 {
			t.Errorf("expected tr:1 second, got %#v", vals[1])
		}
	})

	t.Run("ListFastFieldValues literal wildcard in prefix", func(t *testing.T) {
		vals, err := s.ListFastFieldValuesContext(ctx, "language", ListFastFieldOptions{Prefix: "e_"})
		if err != nil {
			t.Fatalf("literal wildcard prefix: %v", err)
		}
		if len(vals) != 0 {
			t.Errorf("expected 0 matches for 'e_', got %#v", vals)
		}
	})

	t.Run("ListFastFieldValues limit", func(t *testing.T) {
		vals, err := s.ListFastFieldValuesContext(ctx, "tags", ListFastFieldOptions{Limit: 2})
		if err != nil {
			t.Fatalf("ListFastFieldValues tags limit: %v", err)
		}
		if len(vals) != 2 {
			t.Fatalf("expected 2 values with limit 2, got %d", len(vals))
		}
	})

	t.Run("ListFastFieldValues unknown field error", func(t *testing.T) {
		_, err := s.ListFastFieldValuesContext(ctx, "nonexistent", ListFastFieldOptions{})
		if err == nil {
			t.Fatal("expected error for unknown fast field")
		}
		if !strings.Contains(err.Error(), "unknown fast field") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
