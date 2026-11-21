package indexer

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/semantic"
	"github.com/ozgurulukir/seek/internal/store"
)

// Decision-table tests for semanticStateRecorder: the recorder owns every
// documents.semantic_* write for the sync and backfill paths plus the
// memoized identity basis. The end-to-end flows are pinned by the
// TestBackfillSemantic* suites and semantic_test.go; these tests target the
// recorder's own branches so the decision table is visible in one place.

// recorderTestDoc indexes one markdown file with semantic enrichment off and
// returns the store, a mutable config, and the document id.
func recorderTestDoc(t *testing.T) (*store.Store, *config.AppConfig, int64) {
	t.Helper()
	tmp := t.TempDir()
	md := filepath.Join(tmp, "note.md")
	if err := os.WriteFile(md, []byte("# Title\n\nSome go concurrency notes.\n"), 0o644); err != nil {
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
	doc, err := db.GetDocumentContext(context.Background(), col.ID, md)
	if err != nil {
		t.Fatal(err)
	}
	return db, cfg, doc.ID
}

// enabledCfg turns the semantic capability on so the recorder's enabled gate
// lets writes through. The base URL is a loopback port that serves nothing —
// the fake provider below stands in for the real service.
func enabledCfg(cfg *config.AppConfig, url string) *config.AppConfig {
	cfg.Config.Semantic.Enabled = true
	cfg.Config.Semantic.BaseURL = url
	return cfg
}

func healthyFake() *fakeSemanticProvider {
	return &fakeSemanticProvider{healthOk: true, health: semanticHealthAll(), response: fakeTagResult("golang")}
}

// warnCapture is a warnf sink recording each message.
type warnCapture struct {
	mu  sync.Mutex
	got []string
}

func (w *warnCapture) warnf(format string, v ...interface{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.got = append(w.got, format)
}

func semanticStateOf(t *testing.T, db *store.Store, docID int64) store.SemanticState {
	t.Helper()
	states, err := db.GetSemanticStates(context.Background(), []int64{docID})
	if err != nil {
		t.Fatal(err)
	}
	return states[docID]
}

func TestSemanticStateRecorderSyncOutcome(t *testing.T) {
	tests := []struct {
		name string
		// setup runs before the recorder call and may seed state.
		run func(t *testing.T, db *store.Store, cfg *config.AppConfig, docID int64, cap *warnCapture)
		// check runs after the recorder call.
		check func(t *testing.T, db *store.Store, docID int64, cap *warnCapture)
	}{
		{
			name: "success records current with layered fingerprint",
			run: func(t *testing.T, db *store.Store, cfg *config.AppConfig, docID int64, cap *warnCapture) {
				enabledCfg(cfg, "http://127.0.0.1:1")
			},
			check: func(t *testing.T, db *store.Store, docID int64, cap *warnCapture) {
				st := semanticStateOf(t, db, docID)
				if st.Status != store.SemanticStatusCurrent || st.Fingerprint == "" {
					t.Fatalf("state = %+v, want current with a fingerprint", st)
				}
			},
		},
		{
			name: "nil fields while enabled records stale-empty",
			run: func(t *testing.T, db *store.Store, cfg *config.AppConfig, docID int64, cap *warnCapture) {
				enabledCfg(cfg, "http://127.0.0.1:1")
			},
			check: func(t *testing.T, db *store.Store, docID int64, cap *warnCapture) {
				st := semanticStateOf(t, db, docID)
				if st.Status != store.SemanticStatusStale || st.Fingerprint != "" {
					t.Fatalf("state = %+v, want stale with empty fingerprint", st)
				}
			},
		},
		{
			name: "nil fields while disabled records nothing",
			check: func(t *testing.T, db *store.Store, docID int64, cap *warnCapture) {
				st := semanticStateOf(t, db, docID)
				if st.Status != store.SemanticStatusNone || st.Fingerprint != "" {
					t.Fatalf("state = %+v, want untouched none", st)
				}
			},
		},
		{
			name: "ineligible collection type records nothing",
			run: func(t *testing.T, db *store.Store, cfg *config.AppConfig, docID int64, cap *warnCapture) {
				enabledCfg(cfg, "http://127.0.0.1:1")
			},
			check: func(t *testing.T, db *store.Store, docID int64, cap *warnCapture) {
				st := semanticStateOf(t, db, docID)
				if st.Status != store.SemanticStatusNone {
					t.Fatalf("state = %+v, want untouched none", st)
				}
			},
		},
		{
			name: "health failure records nothing and warns once per recorder",
			run: func(t *testing.T, db *store.Store, cfg *config.AppConfig, docID int64, cap *warnCapture) {
				enabledCfg(cfg, "http://127.0.0.1:1")
			},
			check: func(t *testing.T, db *store.Store, docID int64, cap *warnCapture) {
				st := semanticStateOf(t, db, docID)
				if st.Status != store.SemanticStatusNone {
					t.Fatalf("state = %+v, want untouched none after health failure", st)
				}
				if len(cap.got) != 1 {
					t.Fatalf("warnings after two failed calls = %d, want exactly 1 (warn-once)", len(cap.got))
				}
			},
		},
		{
			name: "zero doc id records nothing",
			run: func(t *testing.T, db *store.Store, cfg *config.AppConfig, docID int64, cap *warnCapture) {
				enabledCfg(cfg, "http://127.0.0.1:1")
			},
			check: func(t *testing.T, db *store.Store, docID int64, cap *warnCapture) {
				st := semanticStateOf(t, db, docID)
				if st.Status != store.SemanticStatusNone {
					t.Fatalf("state = %+v, want untouched none", st)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, cfg, docID := recorderTestDoc(t)
			cap := &warnCapture{}
			if tt.run != nil {
				tt.run(t, db, cfg, docID, cap)
			}

			// The healthy fake backs every non-failure case; the failure case
			// deliberately flips healthOk so resolve() reports the outage.
			fake := healthyFake()
			provider := semantic.Provider(fake)
			if tt.name == "health failure records nothing and warns once per recorder" {
				fake.healthOk = false
			}
			rec := newSemanticStateRecorder(db, cfg, func() semantic.Provider { return provider }, cap.warnf)

			chunks := []store.IndexChunk{{Content: "alpha"}, {Content: "beta"}}
			// The eligible type for the sync matrix; the setup collection is
			// markdown (its own type is deliberately not sync-enriched).
			colType := store.CollectionTypeClaude
			docIDArg := docID
			if tt.name == "zero doc id records nothing" {
				docIDArg = 0
			}
			if tt.name == "ineligible collection type records nothing" {
				colType = store.CollectionTypeMarkdown
			}
			fields := map[string]string{"topics": "t1"}
			if tt.name == "nil fields while enabled records stale-empty" ||
				tt.name == "nil fields while disabled records nothing" {
				fields = nil
			}

			// Call twice to exercise the memoized resolve and warn-once paths.
			for i := 0; i < 2; i++ {
				if err := rec.RecordSyncOutcome(context.Background(), colType, docIDArg, fields, chunks); err != nil {
					t.Fatalf("RecordSyncOutcome: %v", err)
				}
			}
			tt.check(t, db, docID, cap)
		})
	}
}

func TestSemanticStateRecorderBackfillWrites(t *testing.T) {
	db, cfg, docID := recorderTestDoc(t)
	enabledCfg(cfg, "http://127.0.0.1:1")
	doc, err := db.GetDocumentByIDContext(context.Background(), docID)
	if err != nil {
		t.Fatal(err)
	}
	chunks := []store.IndexChunk{{Content: "alpha"}}
	ctx := context.Background()

	rec := newSemanticStateRecorder(db, cfg, func() semantic.Provider { return healthyFake() }, nil)

	// Seed an error-state document with prior fast fields, then verify the
	// three backfill write shapes against the store nil-map contract.
	seed := func() {
		t.Helper()
		if err := db.UpdateSemanticState(ctx, docID, "old-fp", "old-basis", doc.ContentHash, store.SemanticStatusError, map[string]string{"tags": "prior-tag"}); err != nil {
			t.Fatal(err)
		}
	}

	// Current with nil fields preserves prior fields (the no-readable-text
	// finalize).
	seed()
	if err := rec.RecordBackfillCurrent(ctx, docID, doc.ContentHash, chunks, nil); err != nil {
		t.Fatalf("RecordBackfillCurrent(nil): %v", err)
	}
	st := semanticStateOf(t, db, docID)
	if st.Status != store.SemanticStatusCurrent || st.Fingerprint == "" {
		t.Fatalf("state = %+v, want current with a fingerprint", st)
	}
	if v, _ := db.FastFields().Get(docID, "tags"); v == nil {
		t.Fatal("nil-field RecordBackfillCurrent must preserve prior fields")
	}

	// Current with an explicit empty map clears prior fields (the successful
	// recompute-to-nothing, review M1).
	seed()
	if err := rec.RecordBackfillCurrent(ctx, docID, doc.ContentHash, chunks, map[string]string{}); err != nil {
		t.Fatalf("RecordBackfillCurrent(empty): %v", err)
	}
	if v, _ := db.FastFields().Get(docID, "tags"); v != nil {
		t.Fatalf("empty-map RecordBackfillCurrent must clear prior fields, got %v", v)
	}

	// Error keeps the full desired fingerprint and preserves fields so the
	// stale query retries the document on the next pass.
	seed()
	if err := rec.RecordBackfillError(ctx, docID, doc.ContentHash, chunks); err != nil {
		t.Fatalf("RecordBackfillError: %v", err)
	}
	st = semanticStateOf(t, db, docID)
	if st.Status != store.SemanticStatusError || st.Fingerprint == "" {
		t.Fatalf("state = %+v, want error with a full fingerprint", st)
	}
	if v, _ := db.FastFields().Get(docID, "tags"); v == nil {
		t.Fatal("RecordBackfillError must preserve prior fields")
	}

	// Unresolved basis: a recorder whose provider is nil must refuse to write
	// a fingerprint against nothing.
	nilRec := newSemanticStateRecorder(db, cfg, func() semantic.Provider { return nil }, nil)
	if err := nilRec.RecordBackfillCurrent(ctx, docID, doc.ContentHash, chunks, nil); err == nil {
		t.Fatal("RecordBackfillCurrent without a provider must error")
	}
}

// countingHealthProvider counts Health calls so the memoized resolve can be
// pinned: one pass, one health check, regardless of how many documents or
// recorder methods consult the basis.
type countingHealthProvider struct {
	inner semantic.Provider
	mu    sync.Mutex
	calls int
}

func (c *countingHealthProvider) Health(ctx context.Context) (semantic.Health, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.inner.Health(ctx)
}

func (c *countingHealthProvider) Tag(ctx context.Context, req semantic.Request) (semantic.Response, error) {
	return c.inner.Tag(ctx, req)
}

func TestSemanticStateRecorderBasisMemoized(t *testing.T) {
	db, cfg, docID := recorderTestDoc(t)
	enabledCfg(cfg, "http://127.0.0.1:1")

	counting := &countingHealthProvider{inner: healthyFake()}
	rec := newSemanticStateRecorder(db, cfg, func() semantic.Provider { return counting }, nil)
	ctx := context.Background()
	chunks := []store.IndexChunk{{Content: "alpha"}}

	// Per-document desired fingerprint, a Current write, an Error write, and
	// another fingerprint must all share the single resolved basis.
	if _, ok := rec.desiredFingerprint(ctx, chunks); !ok {
		t.Fatal("desiredFingerprint reported unresolved basis")
	}
	if _, ok := rec.desiredFingerprint(ctx, chunks); !ok {
		t.Fatal("second desiredFingerprint reported unresolved basis")
	}
	if err := rec.RecordBackfillCurrent(ctx, docID, "h", chunks, nil); err != nil {
		t.Fatalf("RecordBackfillCurrent: %v", err)
	}
	if err := rec.RecordBackfillError(ctx, docID, "h", chunks); err != nil {
		t.Fatalf("RecordBackfillError: %v", err)
	}
	counting.mu.Lock()
	calls := counting.calls
	counting.mu.Unlock()
	if calls != 1 {
		t.Fatalf("Health calls after 4 consults = %d, want 1 (memoized basis)", calls)
	}
}

func TestSemanticStateRecorderProviderSwapResets(t *testing.T) {
	db, cfg, docID := recorderTestDoc(t)
	enabledCfg(cfg, "http://127.0.0.1:1")
	idx := New(cfg, db).WithLogger(nopLogger{})

	before := idx.semanticState()
	idx.WithSemanticProvider(healthyFake())
	after := idx.semanticState()
	if before == after {
		t.Fatal("WithSemanticProvider must reset the semantic-state recorder so a swapped provider is re-identity-checked")
	}

	// A swapped deployment identity must change the recorded fingerprint:
	// mutate the base URL between two successful records.
	ctx := context.Background()
	chunks := []store.IndexChunk{{Content: "alpha"}}
	rec := newSemanticStateRecorder(db, enabledCfg(cfg, "http://127.0.0.1:1"), func() semantic.Provider { return healthyFake() }, nil)
	if err := rec.RecordBackfillCurrent(ctx, docID, "h", chunks, nil); err != nil {
		t.Fatalf("record with identity A: %v", err)
	}
	first := semanticStateOf(t, db, docID)

	rec2 := newSemanticStateRecorder(db, enabledCfg(cfg, "http://127.0.0.1:2"), func() semantic.Provider { return healthyFake() }, nil)
	if err := rec2.RecordBackfillCurrent(ctx, docID, "h", chunks, nil); err != nil {
		t.Fatalf("record with identity B: %v", err)
	}
	second := semanticStateOf(t, db, docID)

	if first.Fingerprint == "" || first.Fingerprint == second.Fingerprint {
		t.Fatalf("fingerprints must differ across deployment identities: %q vs %q", first.Fingerprint, second.Fingerprint)
	}
}
