package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// TestReplaceIndexFastFields: a replace write with fast fields sets them;
// a subsequent replace write with a nil FastFields map must NOT clear them
// (semantic enrichment is optional — a transient outage must not destroy
// previously stored metadata).
func TestReplaceIndexFastFields(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("c", "pdf", "/p", "*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	path := "/p/doc.pdf"

	// First replace: write tags.
	docID, err := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
		CollectionID: col.ID,
		Path:         path,
		Title:        "doc",
		ContentHash:  "h1",
		FTSContent:   "content",
		FastFields:   map[string]string{"tags": "go,rust", "language": "en"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := s.FastFields().Get(docID, "tags"); err != nil || v != "go,rust" {
		t.Fatalf("tags = %v (%v), want go,rust", v, err)
	}

	// Second replace: content changed, same path, FastFields nil (semantic
	// not computed this pass). Existing fast fields must survive.
	docID2, err := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
		CollectionID: col.ID,
		Path:         path,
		Title:        "doc",
		ContentHash:  "h2",
		FTSContent:   "new content",
		// FastFields deliberately nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if docID2 != docID {
		t.Fatalf("expected same doc id, got %d vs %d", docID2, docID)
	}
	if v, err := s.FastFields().Get(docID, "tags"); err != nil || v != "go,rust" {
		t.Fatalf("tags after nil replace = %v (%v), want preserved go,rust", v, err)
	}
	if v, err := s.FastFields().Get(docID, "language"); err != nil || v != "en" {
		t.Fatalf("language after nil replace = %v (%v), want preserved en", v, err)
	}
}

// TestReplaceIndexFastFieldsExplicitClear: a replace with a non-nil (even
// empty) FastFields map DOES clear stale values, so callers can explicitly
// drop metadata when it was actually recomputed to nothing.
func TestReplaceIndexFastFieldsExplicitClear(t *testing.T) {
	s := newTestStore(t)
	col, _ := s.CreateCollection("c", "pdf", "/p", "*.pdf")
	ctx := context.Background()
	path := "/p/doc.pdf"

	docID, _ := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
		CollectionID: col.ID, Path: path, Title: "d", ContentHash: "h1",
		FTSContent: "x", FastFields: map[string]string{"tags": "go"},
	})
	// Recompute to an empty (non-nil) map: stale "tags" must be cleared.
	docID2, _ := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
		CollectionID: col.ID, Path: path, Title: "d", ContentHash: "h2",
		FTSContent: "y", FastFields: map[string]string{},
	})
	if docID2 != docID {
		t.Fatalf("same path expected same id, got %d vs %d", docID2, docID)
	}
	if v, _ := s.FastFields().Get(docID, "tags"); v != nil {
		t.Fatalf("expected tags cleared with explicit empty map, got %v", v)
	}
}

// TestSemanticMigrationAddsColumns verifies the S2 migration appends the
// document-scoped enrichment columns to a fresh database.
func TestSemanticMigrationAddsColumns(t *testing.T) {
	s := newTestStore(t)
	cols := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA table_info(documents)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("PRAGMA rows: %v", err)
	}
	for _, want := range []string{"semantic_fingerprint", "semantic_status"} {
		if !cols[want] {
			t.Errorf("documents missing column %q", want)
		}
	}
}

// TestSemanticMigrationIdempotent verifies re-opening the same database does
// not error on the already-present columns (execIgnoreDuplicate swallows them).
func TestSemanticMigrationIdempotent(t *testing.T) {
	s := newTestStore(t)
	// Re-running the alter statements must be a no-op, not an error.
	if err := s.applyAlterStatements(); err != nil {
		t.Fatalf("re-running alter statements: %v", err)
	}
}

// TestGetStaleSemanticDocuments verifies the batched stale-selection query
// returns documents whose fingerprint differs or is absent, in one query.
func TestGetStaleSemanticDocuments(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("c", "markdown", "/p", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// current: fingerprint matches desired.
	current, err := s.UpsertDocument(col.ID, "/p/current.md", "current", "h1", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSemanticState(ctx, current, "fp-desired", SemanticStatusCurrent, map[string]string{"tags": "go"}); err != nil {
		t.Fatal(err)
	}
	// stale: fingerprint differs.
	stale, err := s.UpsertDocument(col.ID, "/p/stale.md", "stale", "h2", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSemanticState(ctx, stale, "fp-old", SemanticStatusStale, map[string]string{"tags": "rust"}); err != nil {
		t.Fatal(err)
	}
	// absent: never enriched.
	if _, err := s.UpsertDocument(col.ID, "/p/absent.md", "absent", "h3", 1, 1); err != nil {
		t.Fatal(err)
	}

	states, err := s.GetStaleSemanticDocuments(ctx, col.ID, "fp-desired")
	if err != nil {
		t.Fatalf("GetStaleSemanticDocuments: %v", err)
	}
	got := map[int64]bool{}
	for _, st := range states {
		got[st.DocumentID] = true
	}
	if got[current] {
		t.Errorf("current document %d should not be stale", current)
	}
	if !got[stale] {
		t.Errorf("stale document %d should be selected", stale)
	}
	if len(states) != 2 {
		t.Errorf("stale count = %d, want 2 (stale + absent)", len(states))
	}
}

// TestGetSemanticStatesBulk verifies the batched fingerprint read for a set of
// document IDs returns the stored state, with absent enrichment as empty.
func TestGetSemanticStatesBulk(t *testing.T) {
	s := newTestStore(t)
	col, _ := s.CreateCollection("c", "markdown", "/p", "**/*.md")
	ctx := context.Background()

	enriched, _ := s.UpsertDocument(col.ID, "/p/a.md", "a", "h1", 1, 1)
	if err := s.UpdateSemanticState(ctx, enriched, "fp-a", SemanticStatusCurrent, nil); err != nil {
		t.Fatal(err)
	}
	plain, _ := s.UpsertDocument(col.ID, "/p/b.md", "b", "h2", 1, 1)

	states, err := s.GetSemanticStates(ctx, []int64{enriched, plain})
	if err != nil {
		t.Fatalf("GetSemanticStates: %v", err)
	}
	if st := states[enriched]; st.Fingerprint != "fp-a" || st.Status != SemanticStatusCurrent {
		t.Errorf("enriched state = %+v, want fp-a/current", st)
	}
	if st := states[plain]; st.Fingerprint != "" || st.Status != SemanticStatusNone {
		t.Errorf("plain state = %+v, want empty/none", st)
	}
}

// TestUpdateSemanticStateAtomic verifies fast fields and fingerprint/status are
// written together in one transaction.
func TestUpdateSemanticStateAtomic(t *testing.T) {
	s := newTestStore(t)
	col, _ := s.CreateCollection("c", "markdown", "/p", "**/*.md")
	ctx := context.Background()
	docID, _ := s.UpsertDocument(col.ID, "/p/doc.md", "doc", "h1", 1, 1)

	if err := s.UpdateSemanticState(ctx, docID, "fp-1", SemanticStatusCurrent, map[string]string{"tags": "go,rust", "language": "en"}); err != nil {
		t.Fatalf("UpdateSemanticState: %v", err)
	}
	if v, err := s.FastFields().Get(docID, "tags"); err != nil || v != "go,rust" {
		t.Fatalf("tags = %v (%v), want go,rust", v, err)
	}
	states, err := s.GetSemanticStates(ctx, []int64{docID})
	if err != nil {
		t.Fatal(err)
	}
	if st := states[docID]; st.Fingerprint != "fp-1" || st.Status != SemanticStatusCurrent {
		t.Errorf("state = %+v, want fp-1/current", st)
	}
}

// TestUpdateSemanticStatePreservesFieldsOnError verifies that when enrichment
// fails (nil fast fields — service down), the previously stored fast fields are
// preserved and the status reflects the failure rather than silently clearing.
func TestUpdateSemanticStatePreservesFieldsOnError(t *testing.T) {
	s := newTestStore(t)
	col, _ := s.CreateCollection("c", "markdown", "/p", "**/*.md")
	ctx := context.Background()
	docID, _ := s.UpsertDocument(col.ID, "/p/doc.md", "doc", "h1", 1, 1)

	// First pass: enrichment succeeds, writes fields + current state.
	if err := s.UpdateSemanticState(ctx, docID, "fp-1", SemanticStatusCurrent, map[string]string{"tags": "go,rust", "language": "en"}); err != nil {
		t.Fatal(err)
	}

	// Second pass: enrichment fails (nil fast fields). Existing fields must
	// survive and the status must reflect the failure.
	if err := s.UpdateSemanticState(ctx, docID, "fp-1", SemanticStatusError, nil); err != nil {
		t.Fatal(err)
	}
	if v, err := s.FastFields().Get(docID, "tags"); err != nil || v != "go,rust" {
		t.Fatalf("tags after error = %v (%v), want preserved go,rust", v, err)
	}
	if v, err := s.FastFields().Get(docID, "language"); err != nil || v != "en" {
		t.Fatalf("language after error = %v (%v), want preserved en", v, err)
	}
	states, err := s.GetSemanticStates(ctx, []int64{docID})
	if err != nil {
		t.Fatal(err)
	}
	if st := states[docID]; st.Status != SemanticStatusError {
		t.Errorf("status = %q, want error", st.Status)
	}
}

// TestSemanticFingerprintCompute verifies the fingerprint is stable and
// sensitive to each identity component.
func TestSemanticFingerprintCompute(t *testing.T) {
	base := SemanticFingerprint{
		ServiceModel:  "dashscope/qwen3",
		Capabilities:  SemanticCapabilities{Language: true, NER: true, Keyphrase: true, Topic: true},
		SchemaVersion: SemanticSchemaVersion,
		ContentHash:   "abc",
	}
	fp := base.Compute()
	if fp == "" {
		t.Fatal("empty fingerprint")
	}
	if base.Compute() != fp {
		t.Error("fingerprint not stable")
	}
	if strings.EqualFold(base.Compute(), fp) && base.Compute() != fp {
		t.Error("fingerprint case instability")
	}
	// Each component change must produce a different fingerprint.
	variants := []SemanticFingerprint{
		{ServiceModel: "other", Capabilities: base.Capabilities, SchemaVersion: base.SchemaVersion, ContentHash: base.ContentHash},
		{ServiceModel: base.ServiceModel, Capabilities: SemanticCapabilities{Language: true}, SchemaVersion: base.SchemaVersion, ContentHash: base.ContentHash},
		{ServiceModel: base.ServiceModel, Capabilities: base.Capabilities, SchemaVersion: "v2", ContentHash: base.ContentHash},
		{ServiceModel: base.ServiceModel, Capabilities: base.Capabilities, SchemaVersion: base.SchemaVersion, ContentHash: "xyz"},
	}
	for i, v := range variants {
		if v.Compute() == fp {
			t.Errorf("variant %d produced identical fingerprint", i)
		}
	}
}

// TestBackfillReadMethods exercises the read side the semantic backfill is
// built on: GetDocumentByIDContext, ListChunksForDocumentContext, and
// FastFieldStore.ListForDocumentContext. Content is decompressed and fast
// fields decode to plain text with empty values dropped.
func TestBackfillReadMethods(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	col, err := s.CreateCollection("c", "markdown", "/p", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := s.UpsertAndReplaceIndex(ctx, DocumentIndex{
		CollectionID: col.ID,
		Path:         "/p/doc.md",
		Title:        "doc",
		ContentHash:  "h1",
		FTSContent:   "one two three",
		Chunks: []IndexChunk{
			{Seq: 0, Content: "one", StartLine: 1, EndLine: 2},
			{Seq: 1, Content: "two three", StartLine: 3, EndLine: 4},
		},
		FastFields: map[string]string{"tags": "go,web", "language": "en", "empty": ""},
	})
	if err != nil {
		t.Fatal(err)
	}

	doc, err := s.GetDocumentByIDContext(ctx, docID)
	if err != nil {
		t.Fatalf("GetDocumentByIDContext: %v", err)
	}
	if doc.ID != docID || doc.Path != "/p/doc.md" {
		t.Fatalf("doc = %+v, want id %d path /p/doc.md", doc, docID)
	}

	chunks, err := s.ListChunksForDocumentContext(ctx, docID)
	if err != nil {
		t.Fatalf("ListChunksForDocumentContext: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if chunks[0].Seq != 0 || chunks[0].Content != "one" || chunks[0].StartLine != 1 || chunks[0].EndLine != 2 {
		t.Fatalf("chunk 0 = %+v", chunks[0])
	}
	if chunks[1].Seq != 1 || chunks[1].Content != "two three" {
		t.Fatalf("chunk 1 = %+v", chunks[1])
	}
	if chunks[1].ChunkType != ChunkTypeText {
		t.Fatalf("chunk 1 type = %d, want text", chunks[1].ChunkType)
	}

	fields, err := s.FastFields().ListForDocumentContext(ctx, docID)
	if err != nil {
		t.Fatalf("ListForDocumentContext: %v", err)
	}
	if fields["tags"] != "go,web" || fields["language"] != "en" {
		t.Fatalf("fields = %v, want tags=go,web language=en", fields)
	}
	// Empty values are dropped, so an empty placeholder never resurfaces.
	if _, ok := fields["empty"]; ok {
		t.Fatalf("fields = %v, want empty value dropped", fields)
	}
}
