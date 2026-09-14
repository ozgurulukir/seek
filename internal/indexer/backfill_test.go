package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/semantic"
	"github.com/ozgurulukir/seek/internal/store"
)

// fakeSemanticProvider is a controllable semantic.Provider for tests. It
// implements the one-trip contract: Health returns configured models, Tag
// returns the configured response or error.
type fakeSemanticProvider struct {
	mu       sync.Mutex
	health   semantic.Health
	healthOk bool
	// response is returned as-is when tagErr is nil.
	response semantic.Response
	tagErr   error
	// tagHook runs on every Tag call (e.g. to cancel the context mid-loop).
	tagHook func()
}

func (f *fakeSemanticProvider) Health(ctx context.Context) (semantic.Health, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.healthOk {
		return semantic.Health{}, errors.New("fake semantic service is down")
	}
	return f.health, nil
}

func (f *fakeSemanticProvider) Tag(ctx context.Context, req semantic.Request) (semantic.Response, error) {
	f.mu.Lock()
	if f.tagHook != nil {
		f.tagHook()
	}
	tagErr := f.tagErr
	healthOk := f.healthOk
	f.mu.Unlock()
	if !healthOk {
		// A service that reports "down" on /health is down for /tag too:
		// model the outage truthfully instead of returning a success envelope
		// from a dead service.
		return semantic.Response{}, errors.New("fake semantic service is down")
	}
	if tagErr != nil {
		return semantic.Response{}, tagErr
	}
	return f.response, nil
}

func semanticHealthAll() semantic.Health {
	return semantic.Health{Status: "ok", Version: "fake", Models: semantic.Models{
		LID: true, Ner: true, Keyphrase: true, Topic: true,
	}}
}

// fakeTagResult builds a semantic response with one result per chunk carrying
// the given tags.
func fakeTagResult(tags ...string) semantic.Response {
	return semantic.Response{
		Results: []semantic.TagResult{
			{ID: 0, Tags: tags, Topics: []semantic.Topic{{Label: "topic-1", Score: 0.9}}},
		},
		CorpusLang: "en",
	}
}

// openBackfillStore opens a temp store with the FTS5 skip guard, matching the
// established internal-test pattern.
func openBackfillStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled")
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestBackfillSemanticE2E covers the plan's acceptance flow: a markdown
// collection indexed with semantic enrichment off (no semantic fast fields)
// is backfilled once the service is available. After the pass the tags
// fast-field coverage is present and the fingerprint/status is current, and a
// second pass selects nothing (the unchanged document is not stale — the R3
// batched query returns no candidates, so no per-doc chunk reads happen).
func TestBackfillSemanticE2E(t *testing.T) {
	tmp := t.TempDir()
	md := filepath.Join(tmp, "note.md")
	os.WriteFile(md, []byte("# Title\n\nSome go concurrency notes.\n"), 0o644)

	db := openBackfillStore(t)
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmp, "*.md")
	if err != nil {
		t.Fatal(err)
	}

	idx := New(cfg, db).WithLogger(nopLogger{})
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	doc, err := db.GetDocumentContext(context.Background(), col.ID, md)
	if err != nil {
		t.Fatal(err)
	}
	// Semantic enrichment was off: no fingerprint recorded, no semantic fast
	// fields.
	states, err := db.GetSemanticStates(context.Background(), []int64{doc.ID})
	if err != nil {
		t.Fatal(err)
	}
	if st := states[doc.ID]; st.Fingerprint != "" || st.Status != store.SemanticStatusNone {
		t.Fatalf("before backfill state = %+v, want none", st)
	}
	if v, _ := db.FastFields().Get(doc.ID, "tags"); v != nil {
		t.Fatalf("before backfill tags = %v, want none", v)
	}

	// Service comes up: inject the fake provider and run the backfill.
	fake := &fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), response: fakeTagResult("golang", "concurrency")}
	idx.WithSemanticProvider(fake)

	report, err := idx.BackfillSemantic(context.Background(), col, nopLogger{})
	if err != nil {
		t.Fatalf("BackfillSemantic: %v", err)
	}
	if report.Processed != 1 || report.Skipped != 0 || report.Failed != 0 {
		t.Fatalf("backfill report = %+v, want 1 processed", report)
	}

	got, err := db.FastFields().Get(doc.ID, "tags")
	if err != nil || got != "golang,concurrency" {
		t.Fatalf("tags after backfill = %v (%v), want golang,concurrency", got, err)
	}
	states, err = db.GetSemanticStates(context.Background(), []int64{doc.ID})
	if err != nil {
		t.Fatal(err)
	}
	st := states[doc.ID]
	if st.Fingerprint == "" || st.Status != store.SemanticStatusCurrent {
		t.Fatalf("after backfill state = %+v, want current fingerprint", st)
	}

	// The desired fingerprint must be built from the config identity — the
	// same content hash a second pass recomputes.
	chunks, err := db.ListChunksForDocumentContext(context.Background(), doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantFP := store.SemanticFingerprint{
		ServiceModel:  cfg.Config.Semantic.EffectiveBaseURL(),
		Capabilities:  store.SemanticCapabilities{Language: true, NER: true, Keyphrase: true, Topic: true},
		SchemaVersion: store.SemanticSchemaVersion,
		ContentHash:   semanticContentHash(chunks),
	}.Compute()
	if st.Fingerprint != wantFP {
		t.Fatalf("stored fingerprint %q != desired %q", st.Fingerprint, wantFP)
	}

	// A second pass selects nothing: the unchanged document is not stale, so
	// the batched query returns no candidates and no per-doc work (chunk reads)
	// happens — this is the R3 no-N+1 guarantee.
	report2, err := idx.BackfillSemantic(context.Background(), col, nopLogger{})
	if err != nil {
		t.Fatalf("second BackfillSemantic: %v", err)
	}
	if report2.Processed != 0 || report2.Skipped != 0 || report2.Failed != 0 {
		t.Fatalf("second backfill report = %+v, want 0 (nothing selected)", report2)
	}
}

// TestBackfillSemanticDegrade verifies degrade-on-failure: when enrichment
// fails (timeout, malformed response, service down) the previously stored
// fast fields are preserved, the status records the failure, the Failed count
// increments, and the other documents still get processed. (A service that
// answers a VALID but empty envelope is a success recompute-to-nothing, not a
// failure — see TestBackfillSemanticSuccessButEmptyConverges.)
func TestBackfillSemanticDegrade(t *testing.T) {
	for _, tc := range []struct {
		name string
		fake *fakeSemanticProvider
	}{
		{
			name: "timeout",
			fake: &fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), tagErr: context.DeadlineExceeded},
		},
		{
			name: "malformed json",
			fake: &fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), tagErr: errors.New("semantic: decode: invalid character")},
		},
		{
			name: "service down",
			fake: &fakeSemanticProvider{healthOk: false},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			for i, name := range []string{"a.md", "b.md"} {
				if err := os.WriteFile(filepath.Join(tmp, name), []byte(fmt.Sprintf("# %s\n\nBody text %d.\n", name, i)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			db := openBackfillStore(t)
			cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
			col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmp, "*.md")
			if err != nil {
				t.Fatal(err)
			}

			idx := New(cfg, db).WithLogger(nopLogger{})
			if err := idx.SyncCollection(col); err != nil {
				t.Fatal(err)
			}

			// Give doc a prior semantic state so the preserve contract can
			// be asserted. The other doc has no prior state.
			docs := map[string]int64{}
			for path, id := range mustListDocs(t, db, col.ID) {
				docs[path] = id
			}
			prior := docs[filepath.Join(tmp, "a.md")]
			if err := db.UpdateSemanticState(context.Background(), prior, "old-fp", "basis", "h-a", store.SemanticStatusError, map[string]string{"tags": "prior-tag"}); err != nil {
				t.Fatal(err)
			}

			idx.WithSemanticProvider(tc.fake)
			report, err := idx.BackfillSemantic(context.Background(), col, nopLogger{})
			if err != nil {
				t.Fatalf("BackfillSemantic: %v", err)
			}
			// Both documents fail (no usable enrichment), and — critically —
			// the pass continues past the first failure.
			if report.Failed != 2 {
				t.Fatalf("report = %+v, want 2 failed (backfill did not continue)", report)
			}
			if report.Processed != 0 {
				t.Fatalf("report = %+v, want 0 processed", report)
			}

			// Prior fast fields survive the failure.
			v, err := db.FastFields().Get(prior, "tags")
			if err != nil || v != "prior-tag" {
				t.Fatalf("prior tags = %v (%v), want preserved prior-tag", v, err)
			}
			states, err := db.GetSemanticStates(context.Background(), []int64{prior})
			if err != nil {
				t.Fatal(err)
			}
			if st := states[prior]; st.Status != store.SemanticStatusError {
				t.Fatalf("status = %q, want error", st.Status)
			}
		})
	}
}

// mustListDocs returns collection paths → doc IDs, failing the test on error.
func mustListDocs(t *testing.T, db *store.Store, colID int64) map[string]int64 {
	t.Helper()
	docs, err := db.ListDocumentPathsContext(context.Background(), colID)
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

// TestBackfillSemanticRetriesAfterFailure verifies the degrade contract's
// retry path: a document that failed during one pass (status error) is
// re-selected and re-enriched on the next pass once the service recovers.
func TestBackfillSemanticRetriesAfterFailure(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "note.md"), []byte("# Title\n\nBody text.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db := openBackfillStore(t)
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmp, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	idx := New(cfg, db).WithLogger(nopLogger{})
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	doc, err := db.GetDocumentContext(context.Background(), col.ID, filepath.Join(tmp, "note.md"))
	if err != nil {
		t.Fatal(err)
	}

	// First pass: service is down. The doc fails with status error and its
	// fingerprint records the desired identity+content hash (not the
	// identity-only basis), so it stays selectable on the next pass.
	down := &fakeSemanticProvider{healthOk: false}
	idx.WithSemanticProvider(down)
	if _, err := idx.BackfillSemantic(context.Background(), col, nopLogger{}); err != nil {
		t.Fatalf("degraded backfill: %v", err)
	}
	states, err := db.GetSemanticStates(context.Background(), []int64{doc.ID})
	if err != nil {
		t.Fatal(err)
	}
	if st := states[doc.ID]; st.Status != store.SemanticStatusError || st.Fingerprint == "" {
		t.Fatalf("after failure state = %+v, want error with recorded fingerprint", st)
	}

	// Second pass: service recovered. The failed doc is retried and becomes
	// current.
	healthy := &fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), response: fakeTagResult("golang")}
	idx.WithSemanticProvider(healthy)
	report, err := idx.BackfillSemantic(context.Background(), col, nopLogger{})
	if err != nil {
		t.Fatalf("recovered backfill: %v", err)
	}
	if report.Processed != 1 || report.Skipped != 0 || report.Failed != 0 {
		t.Fatalf("recovered report = %+v, want 1 processed", report)
	}
	v, err := db.FastFields().Get(doc.ID, "tags")
	if err != nil || v != "golang" {
		t.Fatalf("tags after recovery = %v (%v), want golang", v, err)
	}
	states, err = db.GetSemanticStates(context.Background(), []int64{doc.ID})
	if err != nil {
		t.Fatal(err)
	}
	if st := states[doc.ID]; st.Status != store.SemanticStatusCurrent {
		t.Fatalf("after recovery state = %+v, want current", st)
	}
}

// TestBackfillSemanticSuccessButEmptyConverges pins the review-M1 contract:
// a SUCCESSFUL enrichment that recomputes to nothing (service up, but no
// tags/topics/entities and empty/unknown corpus language) is NOT a failure.
// The document is marked current with the full desired fingerprint, the
// previously stored semantic fields are cleared (recomputed-to-nothing, not
// preserved), the pass counts it as processed, and a second pass does not
// re-select or re-send it.
func TestBackfillSemanticSuccessButEmptyConverges(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "note.md"), []byte("# Title\n\nBody text.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db := openBackfillStore(t)
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmp, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	idx := New(cfg, db).WithLogger(nopLogger{})
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	doc, err := db.GetDocumentContext(context.Background(), col.ID, filepath.Join(tmp, "note.md"))
	if err != nil {
		t.Fatal(err)
	}

	// Seed a prior enrichment so the recompute-to-nothing MUST clear it, not
	// preserve it (the exact conflation review M1 rejected: len(fields)==0
	// treated "success but empty" as a failure and kept the old values). Only
	// semantic-owned fields are seeded — tags is dual-owned and treated as
	// native in the backfill merge, so it is not the clearing contract under
	// test here.
	if err := db.UpdateSemanticState(context.Background(), doc.ID, "old-fp", "basis", "h", store.SemanticStatusStale, map[string]string{
		"topics":   "stale-topic",
		"entities": "ORG:StaleOrg",
		"language": "en",
	}); err != nil {
		t.Fatal(err)
	}

	// The service is healthy but its response carries no tags/topics/entities
	// and an unknown/empty corpus language → non-nil EMPTY fields.
	var tagCount tagCounter
	fake := &fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), response: semantic.Response{CorpusLang: "unknown"}}
	fake.tagHook = func() { tagCount.inc() }
	idx.WithSemanticProvider(fake)

	report, err := idx.BackfillSemantic(context.Background(), col, nopLogger{})
	if err != nil {
		t.Fatalf("BackfillSemantic: %v", err)
	}
	if report.Processed != 1 || report.Failed != 0 {
		t.Fatalf("first pass report = %+v, want 1 processed, 0 failed (success-but-empty is not a failure)", report)
	}

	// Status current with the full desired fingerprint — the doc converges.
	states, err := db.GetSemanticStates(context.Background(), []int64{doc.ID})
	if err != nil {
		t.Fatal(err)
	}
	st := states[doc.ID]
	if st.Fingerprint == "" || st.Status != store.SemanticStatusCurrent {
		t.Fatalf("after empty recompute state = %+v, want current with fingerprint", st)
	}
	chunks, err := db.ListChunksForDocumentContext(context.Background(), doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantFP := store.SemanticFingerprint{
		ServiceModel:  cfg.Config.Semantic.EffectiveBaseURL(),
		Capabilities:  store.SemanticCapabilities{Language: true, NER: true, Keyphrase: true, Topic: true},
		SchemaVersion: store.SemanticSchemaVersion,
		ContentHash:   semanticContentHash(chunks),
	}.Compute()
	if st.Fingerprint != wantFP {
		t.Fatalf("fingerprint %q != desired %q (doc must converge on the desired identity)", st.Fingerprint, wantFP)
	}

	// Recomputed-to-nothing CLEARS the stale semantic-owned fields (non-nil
	// empty map = explicit clear); there is no native field to keep.
	for field := range map[string]string{"topics": "", "entities": "", "language": ""} {
		if v, _ := db.FastFields().Get(doc.ID, field); v != nil {
			t.Errorf("field %q = %v after empty recompute, want cleared", field, v)
		}
	}

	// Second pass: the doc is no longer stale (basis, source hash, and status
	// all match), so the batched query selects no candidates — the service is
	// never re-called and the pass reports no processed/failed work (the R3
	// no-N+1 guarantee).
	report2, err := idx.BackfillSemantic(context.Background(), col, nopLogger{})
	if err != nil {
		t.Fatalf("second BackfillSemantic: %v", err)
	}
	if report2.Processed != 0 || report2.Failed != 0 {
		t.Fatalf("second pass report = %+v, want 0 processed/failed (converged)", report2)
	}
	if tagCount.n != 1 {
		t.Fatalf("Tag calls = %d, want exactly 1 (second pass must not re-send the converged doc)", tagCount.n)
	}
}

// TestBackfillSemanticRefreshReplacesStaleSemanticFields is the regression
// test for the merge-conflation defect: a re-backfill after a semantic
// capability change must REPLACE the previously persisted semantic
// topics/entities/language with the freshly computed values, while keeping the
// source-derived (native) fields such as frontmatter tags. Before the fix the
// persisted semantic values were fed into the native side of the merge and
// "native wins on conflict" retained the stale topics/entities/language.
func TestBackfillSemanticRefreshReplacesStaleSemanticFields(t *testing.T) {
	tmp := t.TempDir()
	md := filepath.Join(tmp, "note.md")
	// Frontmatter tags are source-derived (native) and must survive both
	// backfill passes.
	os.WriteFile(md, []byte("---\ntags: user-tag\n---\n# Title\n\nGo concurrency body.\n"), 0o644)

	db := openBackfillStore(t)
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmp, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	idx := New(cfg, db).WithLogger(nopLogger{})
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	doc, err := db.GetDocumentContext(context.Background(), col.ID, md)
	if err != nil {
		t.Fatal(err)
	}

	// First pass: full capabilities, old semantic values.
	oldResp := fakeRichResponse(
		[]string{"golang"},
		[]string{"old-topic"},
		[]semantic.Entity{{Type: "ORG", Text: "OldOrg"}},
		"tr",
	)
	idx.WithSemanticProvider(&fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), response: oldResp})
	report, err := idx.BackfillSemantic(context.Background(), col, nopLogger{})
	if err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	if report.Processed != 1 || report.Failed != 0 {
		t.Fatalf("first backfill report = %+v, want 1 processed", report)
	}
	for field, want := range map[string]string{
		"tags":     "user-tag,golang",
		"topics":   "old-topic",
		"entities": "ORG:OldOrg",
		"language": "tr",
	} {
		if v, _ := db.FastFields().Get(doc.ID, field); v != want {
			t.Fatalf("first pass %s = %v, want %q", field, v, want)
		}
	}

	// Second pass: capability set changes (NER off ⇒ different fingerprint
	// basis ⇒ documents are stale) and the new model produces different
	// semantic values.
	newResp := fakeRichResponse(
		[]string{"golang", "concurrency"},
		[]string{"new-topic"},
		[]semantic.Entity{{Type: "ORG", Text: "NewOrg"}},
		"en",
	)
	changedCaps := semantic.Health{Status: "ok", Models: semantic.Models{
		LID: true, Ner: false, Keyphrase: true, Topic: true,
	}}
	idx.WithSemanticProvider(&fakeSemanticProvider{healthOk: true, health: changedCaps, response: newResp})
	report, err = idx.BackfillSemantic(context.Background(), col, nopLogger{})
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if report.Processed != 1 || report.Skipped != 0 || report.Failed != 0 {
		t.Fatalf("second backfill report = %+v, want 1 processed (re-enriched)", report)
	}

	// The semantic-owned fields are REPLACED by the fresh values…
	for field, want := range map[string]string{
		"topics":   "new-topic",
		"entities": "ORG:NewOrg",
		"language": "en",
	} {
		if v, _ := db.FastFields().Get(doc.ID, field); v != want {
			t.Fatalf("after refresh %s = %v, want %q (stale value retained)", field, v, want)
		}
	}
	// …while the source-derived frontmatter tags survive the merge and the
	// new semantic tags are merged in.
	if v, _ := db.FastFields().Get(doc.ID, "tags"); v != "user-tag,golang,concurrency" {
		t.Fatalf("tags after refresh = %v, want user-tag,golang,concurrency", v)
	}
}

// fakeRichResponse builds a semantic response that carries tags, topics,
// entities, and a corpus language so tests can exercise every semantic-owned
// fast field.
func fakeRichResponse(tags []string, topicLabels []string, entities []semantic.Entity, lang string) semantic.Response {
	topics := make([]semantic.Topic, 0, len(topicLabels))
	for _, label := range topicLabels {
		topics = append(topics, semantic.Topic{Label: label, Score: 0.9})
	}
	return semantic.Response{
		Results:    []semantic.TagResult{{ID: 0, Tags: tags, Topics: topics, Entities: entities}},
		CorpusLang: lang,
	}
}

// TestBackfillSemanticPreservesOtherFieldsOnSuccess verifies the merged
// write keeps non-semantic fast fields (e.g. markdown frontmatter tags)
// alongside the generated semantic ones.
func TestBackfillSemanticPreservesOtherFieldsOnSuccess(t *testing.T) {
	tmp := t.TempDir()
	md := filepath.Join(tmp, "note.md")
	// Frontmatter tags are native fast fields written by the markdown sync.
	content := "---\ntags: user-tag\n---\n# Title\n\nGo concurrency body.\n"
	os.WriteFile(md, []byte(content), 0o644)

	db := openBackfillStore(t)
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmp, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	idx := New(cfg, db).WithLogger(nopLogger{})
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	doc, err := db.GetDocumentContext(context.Background(), col.ID, md)
	if err != nil {
		t.Fatal(err)
	}
	// Native frontmatter tags are stored before the backfill.
	if v, _ := db.FastFields().Get(doc.ID, "tags"); v != "user-tag" {
		t.Fatalf("native tags = %v, want user-tag", v)
	}

	fake := &fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), response: fakeTagResult("golang")}
	idx.WithSemanticProvider(fake)
	report, err := idx.BackfillSemantic(context.Background(), col, nopLogger{})
	if err != nil {
		t.Fatalf("BackfillSemantic: %v", err)
	}
	if report.Processed != 1 || report.Failed != 0 {
		t.Fatalf("report = %+v, want 1 processed", report)
	}
	// Frontmatter user-tag and the generated golang tag coexist after merge.
	v, err := db.FastFields().Get(doc.ID, "tags")
	if err != nil || v != "user-tag,golang" {
		t.Fatalf("merged tags = %v (%v), want user-tag,golang", v, err)
	}
}

// TestBackfillSemanticIsolation is the --semantic-only proof: after a
// backfill pass the chunk contents, the FTS rowset, and the vector search
// results are byte-for-byte identical. The only writes are fast fields +
// fingerprint/status via UpdateSemanticState.
func TestBackfillSemanticIsolation(t *testing.T) {
	tmp := t.TempDir()
	md := filepath.Join(tmp, "note.md")
	os.WriteFile(md, []byte("# Title\n\nGo concurrency notes body.\n"), 0o644)

	db := openBackfillStore(t)
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	// Configure a linear vector index with a matching dimension so vector
	// search works without a profile record (desired profile left nil).
	lcfg := &config.AppConfig{
		Config: config.Config{
			VectorIndex: config.VectorIndexConfig{Backend: "linear"},
			Embedding:   config.EmbeddingConfig{Dimensions: 3},
		},
	}
	vidx, err := store.NewVectorIndex(lcfg)
	if err != nil {
		t.Fatal(err)
	}
	db.SetVectorIndex(vidx)

	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmp, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	idx := New(cfg, db).WithLogger(nopLogger{})
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	doc, err := db.GetDocumentContext(context.Background(), col.ID, md)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := db.GetChunksWithoutEmbeddingForCollectionContext(context.Background(), col.ID, true)
	if err != nil || len(pending) == 0 {
		t.Fatalf("chunks = %d (%v), want > 0", len(pending), err)
	}
	// Give the first chunk a known 3-dim embedding so vector search has
	// something to retrieve both before and after the backfill.
	queryEmb := []float32{0.1, 0.2, 0.3}
	embedChunkID := pending[0].ID
	if err := db.UpdateChunkEmbedding(embedChunkID, queryEmb); err != nil {
		t.Fatal(err)
	}
	if err := db.SyncVectorIndex(); err != nil {
		t.Fatal(err)
	}

	// Snapshot the pre-backfill state.
	chunksBefore := mustChunkContents(t, db, doc.ID)
	ftsBefore := mustFTSResults(t, db, "concurrency")
	vecBefore, err := db.SearchVectorContext(context.Background(), queryEmb, 10, nil)
	if err != nil {
		t.Fatalf("SearchVectorContext before: %v", err)
	}

	// Run the backfill with a healthy fake provider.
	fake := &fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), response: fakeTagResult("golang")}
	idx.WithSemanticProvider(fake)
	report, err := idx.BackfillSemantic(context.Background(), col, nopLogger{})
	if err != nil {
		t.Fatalf("BackfillSemantic: %v", err)
	}
	if report.Processed != 1 || report.Failed != 0 {
		t.Fatalf("report = %+v, want 1 processed", report)
	}

	// Chunk contents identical.
	chunksAfter := mustChunkContents(t, db, doc.ID)
	if len(chunksAfter) != len(chunksBefore) {
		t.Fatalf("chunk count changed: %d → %d", len(chunksBefore), len(chunksAfter))
	}
	for i := range chunksBefore {
		if chunksBefore[i] != chunksAfter[i] {
			t.Fatalf("chunk %d content changed by backfill:\nbefore %q\nafter  %q", i, chunksBefore[i], chunksAfter[i])
		}
	}

	// FTS rowset identical.
	ftsAfter := mustFTSResults(t, db, "concurrency")
	if !equalSearchResults(ftsBefore, ftsAfter) {
		t.Fatalf("FTS results changed by backfill: before %+v, after %+v", ftsBefore, ftsAfter)
	}

	// Vector search results identical — embeddings and the vector index were
	// untouched.
	vecAfter, err := db.SearchVectorContext(context.Background(), queryEmb, 10, nil)
	if err != nil {
		t.Fatalf("SearchVectorContext after: %v", err)
	}
	if !equalSearchResults(vecBefore, vecAfter) {
		t.Fatalf("vector results changed by backfill: before %+v, after %+v", vecBefore, vecAfter)
	}
}

// TestBackfillSemanticCancellation verifies that a cancelled context stops the
// loop at the next document boundary without corrupting state.
func TestBackfillSemanticCancellation(t *testing.T) {
	tmp := t.TempDir()
	// Two documents so a mid-loop cancel leaves the second untouched.
	for _, name := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte("# "+name+"\n\nBody.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db := openBackfillStore(t)
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmp, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	idx := New(cfg, db).WithLogger(nopLogger{})
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	docs := mustListDocs(t, db, col.ID)
	if len(docs) != 2 {
		t.Fatalf("docs = %d, want 2", len(docs))
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	// Cancel on the SECOND Tag call: the first document's enrichment and
	// state write complete, then the loop stops before the second persists.
	var tagCount tagCounter
	fake := &fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), response: fakeTagResult("tag")}
	fake.tagHook = func() {
		if tagCount.inc() == 2 {
			cancel()
		}
	}

	idx.WithSemanticProvider(fake)
	report, err := idx.BackfillSemantic(cancelCtx, col, nopLogger{})
	// The loop either completed the first doc and returned ctx.Err(), or the
	// tags call observed the cancellation. Either way the pass must return the
	// cancellation error and never complete both documents.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("BackfillSemantic error = %v, want context.Canceled", err)
	}
	if report.Processed+report.Failed != 1 {
		t.Fatalf("report = %+v, want exactly one document touched before cancel", report)
	}

	// State is not corrupted: exactly the still-pending document remains
	// unenriched; no partial fingerprint is written for it.
	states, err := db.GetSemanticStates(context.Background(), docIDs(docs))
	if err != nil {
		t.Fatal(err)
	}
	touched := 0
	for _, st := range states {
		if st.Fingerprint != "" || st.Status != store.SemanticStatusNone {
			touched++
		}
	}
	if touched != 1 {
		t.Fatalf("documents with recorded state = %d, want 1 (only the first)", touched)
	}
}

// TestBackfillSemanticContentChangeReSelected verifies the R3 content-change
// path end to end: after the source content changes and the collection is
// re-synced (documents.content_hash updates while the stored semantic_source_hash
// still holds the old value), the batched stale query re-selects the document
// via the source-hash condition and the per-doc check re-enriches it — the chunk
// hash changed, so it is not skipped.
func TestBackfillSemanticContentChangeReSelected(t *testing.T) {
	tmp := t.TempDir()
	md := filepath.Join(tmp, "note.md")
	os.WriteFile(md, []byte("# Title\n\nOriginal body text.\n"), 0o644)

	db := openBackfillStore(t)
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmp, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	idx := New(cfg, db).WithLogger(nopLogger{})
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	doc, err := db.GetDocumentContext(context.Background(), col.ID, md)
	if err != nil {
		t.Fatal(err)
	}

	fake := &fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), response: fakeTagResult("golang")}
	idx.WithSemanticProvider(fake)

	// First backfill: enrich to current.
	if report, err := idx.BackfillSemantic(context.Background(), col, nopLogger{}); err != nil {
		t.Fatalf("first backfill: %v", err)
	} else if report.Processed != 1 {
		t.Fatalf("first backfill report = %+v, want 1 processed", report)
	}

	// Edit the source content and re-sync: documents.content_hash changes while
	// the readable chunk contents change too.
	os.WriteFile(md, []byte("# Title\n\nCompletely different body text here.\n"), 0o644)
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}

	// Second backfill: the batched query re-selects via the source-hash condition
	// and the per-doc check re-enriches (chunk hash changed, so not skipped).
	report2, err := idx.BackfillSemantic(context.Background(), col, nopLogger{})
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if report2.Processed != 1 || report2.Skipped != 0 || report2.Failed != 0 {
		t.Fatalf("second backfill report = %+v, want 1 processed (re-enriched)", report2)
	}
	// The re-enrichment replaced the tags fast field.
	if v, _ := db.FastFields().Get(doc.ID, "tags"); v != "golang" {
		t.Fatalf("tags after content-change re-enrich = %v, want golang", v)
	}
}

// TestBackfillSemanticOverSelectionSafe verifies the over-selection safety of
// the R3 design: when documents.content_hash changes but the readable chunk
// contents (and thus the chunk-based content hash) do not — the SQL-side signal
// of a whitespace-only source edit — the batched query still re-selects the
// document via the source-hash condition, but the per-doc check precisely skips
// it (Skipped) because the chunk fingerprint is unchanged and the status is
// current. Over-selection is safe: no needless re-enrichment.
func TestBackfillSemanticOverSelectionSafe(t *testing.T) {
	tmp := t.TempDir()
	md := filepath.Join(tmp, "note.md")
	os.WriteFile(md, []byte("# Title\n\nBody text.\n"), 0o644)

	db := openBackfillStore(t)
	cfg := cfgFromTest(t, tmp, filepath.Join(tmp, "test.db"))
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmp, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	idx := New(cfg, db).WithLogger(nopLogger{})
	if err := idx.SyncCollection(col); err != nil {
		t.Fatal(err)
	}
	doc, err := db.GetDocumentContext(context.Background(), col.ID, md)
	if err != nil {
		t.Fatal(err)
	}

	fake := &fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), response: fakeTagResult("golang")}
	idx.WithSemanticProvider(fake)
	if report, err := idx.BackfillSemantic(context.Background(), col, nopLogger{}); err != nil {
		t.Fatalf("backfill: %v", err)
	} else if report.Processed != 1 {
		t.Fatalf("backfill report = %+v, want 1 processed", report)
	}

	// Simulate a source change that updates documents.content_hash but leaves the
	// readable chunk contents (and thus the chunk-based content hash) unchanged —
	// a whitespace-only edit. The document content hash is bumped directly; the
	// chunks are untouched.
	if err := db.UpdateDocumentContentHashContext(context.Background(), doc.ID, "changed-content-hash"); err != nil {
		t.Fatal(err)
	}

	// The batched query re-selects the document (source-hash mismatch), but the
	// per-doc check precisely skips it: the chunk fingerprint is unchanged and
	// the status is current.
	report2, err := idx.BackfillSemantic(context.Background(), col, nopLogger{})
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if report2.Processed != 0 || report2.Skipped != 1 || report2.Failed != 0 {
		t.Fatalf("second backfill report = %+v, want 1 skipped (over-selection safe)", report2)
	}
}

// --- helpers ---

func mustChunkContents(t *testing.T, db *store.Store, docID int64) []string {
	t.Helper()
	chunks, err := db.ListChunksForDocumentContext(context.Background(), docID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range chunks {
		out = append(out, c.Content)
	}
	return out
}

func mustFTSResults(t *testing.T, db *store.Store, query string) []store.SearchResult {
	t.Helper()
	results, err := db.SearchFTSContext(context.Background(), query, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	return results
}

func equalSearchResults(a, b []store.SearchResult) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].DocumentID != b[i].DocumentID || a[i].ChunkID != b[i].ChunkID || a[i].Content != b[i].Content {
			return false
		}
	}
	return true
}

func docIDs(docs map[string]int64) []int64 {
	out := make([]int64, 0, len(docs))
	for _, id := range docs {
		out = append(out, id)
	}
	return out
}

// tagCounter is a concurrency-safe counter used to trigger context
// cancellation on a specific Tag invocation.
type tagCounter struct {
	mu sync.Mutex
	n  int
}

func (c *tagCounter) inc() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return c.n
}
