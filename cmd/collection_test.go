package cmd_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/cmd"
	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/semantic"
	"github.com/ozgurulukir/seek/internal/store"
)

// collectionTestConfig builds the AppConfig used by the S5 command tests. The
// vector backend is fixed to linear so the full runtime stays deterministic
// and off-disk; CacheDir lets the writer lock land in the temp dir.
func collectionTestConfig(t *testing.T, dbPath string) *config.AppConfig {
	t.Helper()
	return &config.AppConfig{
		Config: config.Config{
			VectorIndex: config.VectorIndexConfig{Backend: "linear"},
			// Semantic counts in list/show come from the store; embedding is
			// left unconfigured so the runtime builds no network clients.
		},
		DBPath:   dbPath,
		CacheDir: filepath.Join(t.TempDir(), "cache"),
	}
}

func TestCollectionRenameCmd_E2E(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := collectionTestConfig(t, dbPath)
	// The embedded vector below is 3-dimensional; the runtime defaults to
	// 1024 when dimensions are unset, so pin it (same as the app-level test).
	cfg.Config.Embedding = config.EmbeddingConfig{Dimensions: 3}

	// Seed a collection with a document, FTS row, and embedded chunk, then
	// close so the rename command can take the writer lock and reopen.
	rt, err := app.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	col, err := rt.Store.CreateCollection("old", store.CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := rt.Store.UpsertDocument(col.ID, "/tmp/note.md", "Architecture", "hash", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Store.UpsertFTS(docID, "Architecture", "runtime composition"); err != nil {
		t.Fatal(err)
	}
	embed := []float32{0.1, 0.2, 0.3}
	if err := rt.Store.InsertChunk(docID, 0, "runtime composition", embed); err != nil {
		t.Fatal(err)
	}
	if err := rt.Store.SyncVectorIndex(); err != nil {
		t.Fatalf("SyncVectorIndex: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}

	if err := (&cmd.CollectionRenameCmd{Old: "old", New: "new"}).Run(cfg); err != nil {
		t.Fatalf("CollectionRenameCmd.Run: %v", err)
	}

	rt2, err := app.Open(cfg)
	if err != nil {
		t.Fatalf("Open after rename: %v", err)
	}
	defer rt2.Close()

	// Old name is gone; new name resolves; counts are untouched.
	if _, err := rt2.Store.GetCollectionByName("old"); err == nil {
		t.Error("old collection name still resolves after rename")
	}
	newCol, err := rt2.Store.GetCollectionByName("new")
	if err != nil {
		t.Fatalf("new collection missing after rename: %v", err)
	}
	docs, _ := rt2.Store.CountDocuments(newCol.ID)
	chunks, _ := rt2.Store.CountChunks(newCol.ID)
	if docs != 1 || chunks != 1 {
		t.Errorf("counts changed by rename: docs=%d chunks=%d, want 1/1", docs, chunks)
	}

	// Search results carry the new collection name (BM25 and vector).
	bm25, err := rt2.Search.SearchBM25(context.Background(), "composition", 10, search.Options{})
	if err != nil {
		t.Fatalf("SearchBM25: %v", err)
	}
	if len(bm25) != 1 || bm25[0].Collection != "new" {
		t.Errorf("BM25 results = %+v, want one result in %q", bm25, "new")
	}
	vec, err := rt2.Store.SearchVectorContext(context.Background(), embed, 10, nil)
	if err != nil {
		t.Fatalf("SearchVectorContext: %v", err)
	}
	if len(vec) != 1 || vec[0].Collection != "new" {
		t.Errorf("vector results = %+v, want one result in %q", vec, "new")
	}
}

func TestCollectionRenameCmd_ConflictIsClear(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := collectionTestConfig(t, dbPath)

	db := openTestStore(t, dbPath)
	if _, err := db.CreateCollection("alpha", store.CollectionTypeMarkdown, "/tmp", "**/*.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateCollection("beta", store.CollectionTypeMarkdown, "/tmp", "**/*.md"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	err := (&cmd.CollectionRenameCmd{Old: "alpha", New: "beta"}).Run(cfg)
	if err == nil {
		t.Fatal("rename to an existing name should fail")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("conflict error should explain the clash, got: %v", err)
	}

	// Neither rename nor its message touched the source labels.
	db2 := openTestStore(t, dbPath)
	defer db2.Close()
	if _, err := db2.GetCollectionByName("alpha"); err != nil {
		t.Errorf("alpha should still exist: %v", err)
	}
	if _, err := db2.GetCollectionByName("beta"); err != nil {
		t.Errorf("beta should still exist: %v", err)
	}
}

func TestCollectionRenameCmd_SameNameIsNoop(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := collectionTestConfig(t, dbPath)

	db := openTestStore(t, dbPath)
	col, err := db.CreateCollection("alpha", store.CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := db.UpsertDocument(col.ID, "/tmp/note.md", "Note", "h", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertChunk(docID, 0, "content", nil); err != nil {
		t.Fatal(err)
	}
	db.Close()

	var runErr error
	out := captureStdout(t, func() {
		runErr = (&cmd.CollectionRenameCmd{Old: "alpha", New: "alpha"}).Run(cfg)
	})
	if runErr != nil {
		t.Fatalf("rename to same name = %v, want no-op", runErr)
	}
	if !strings.Contains(out, "unchanged") {
		t.Errorf("same-name rename should report no-op, got: %s", out)
	}

	// The collection, documents, and chunks are untouched.
	db2 := openTestStore(t, dbPath)
	defer db2.Close()
	got, err := db2.GetCollectionByName("alpha")
	if err != nil {
		t.Fatalf("alpha missing after no-op rename: %v", err)
	}
	docs, _ := db2.CountDocuments(got.ID)
	chunks, _ := db2.CountChunks(got.ID)
	if docs != 1 || chunks != 1 {
		t.Errorf("counts changed by no-op rename: docs=%d chunks=%d, want 1/1", docs, chunks)
	}
}

func TestCollectionListCmd_ShowsSemanticCoverage(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := collectionTestConfig(t, dbPath)

	db := openTestStore(t, dbPath)
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, filepath.Join(tmpDir, "notes"), "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := db.UpsertDocument(col.ID, filepath.Join(tmpDir, "notes", "note.md"), "Note", "h", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := db.UpsertDocument(col.ID, filepath.Join(tmpDir, "notes", "b.md"), "b", "h2", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertDocument(col.ID, filepath.Join(tmpDir, "notes", "c.md"), "c", "h3", 1, 1); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.UpdateSemanticState(ctx, docID, "fp-cur", store.SemanticStatusCurrent, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateSemanticState(ctx, d2, "fp-err", store.SemanticStatusError, nil); err != nil {
		t.Fatal(err)
	}
	// The third document has no recorded state → semantic none.
	db.Close()

	var runErr error
	out := captureStdout(t, func() {
		runErr = (&cmd.CollectionListCmd{}).Run(cfg)
	})
	if runErr != nil {
		t.Fatalf("CollectionListCmd.Run: %v", runErr)
	}
	for _, want := range []string{"notes", "type=", "docs=", "chunks=", "embedded=", "semantic=", "current/stale/error/none"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
	// Coverage: 1 current, 1 error, 1 none, 0 stale → "1/0/1/1".
	if !strings.Contains(out, "1/0/1/1") {
		t.Errorf("list output missing expected coverage 1/0/1/1:\n%s", out)
	}
}

func TestCollectionListCmd_EmptyDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := collectionTestConfig(t, dbPath)
	db := openTestStore(t, dbPath)
	db.Close()

	var runErr error
	out := captureStdout(t, func() {
		runErr = (&cmd.CollectionListCmd{}).Run(cfg)
	})
	if runErr != nil {
		t.Fatalf("CollectionListCmd.Run: %v", runErr)
	}
	if !strings.Contains(out, "No collections") {
		t.Errorf("empty list should say no collections, got: %s", out)
	}
}

func TestCollectionListCmd_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := collectionTestConfig(t, dbPath)

	db := openTestStore(t, dbPath)
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, filepath.Join(tmpDir, "notes"), "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := db.UpsertDocument(col.ID, filepath.Join(tmpDir, "notes", "note.md"), "Note", "h", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateSemanticState(context.Background(), docID, "fp-cur", store.SemanticStatusCurrent, nil); err != nil {
		t.Fatal(err)
	}
	db.Close()

	var runErr error
	out := captureStdout(t, func() {
		runErr = (&cmd.CollectionListCmd{JSON: true}).Run(cfg)
	})
	if runErr != nil {
		t.Fatalf("CollectionListCmd.Run JSON: %v", runErr)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("unmarshal list json: %v (raw: %s)", err, out)
	}
	if len(list) != 1 || list[0]["name"] != "notes" {
		t.Fatalf("list json = %v, want one notes collection", list)
	}
	for _, key := range []string{"type", "path", "pattern", "documents", "chunks", "embedded_chunks", "semantic_current", "semantic_stale", "semantic_error", "semantic_none"} {
		if _, ok := list[0][key]; !ok {
			t.Errorf("list json missing %q key: %v", key, list[0])
		}
	}
}

func TestCollectionShowCmd_Output(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := collectionTestConfig(t, dbPath)

	db := openTestStore(t, dbPath)
	if _, err := db.CreateCollection("notes", store.CollectionTypeParser, filepath.Join(tmpDir, "sessions"), "**/*"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	var runErr error
	out := captureStdout(t, func() {
		runErr = (&cmd.CollectionShowCmd{Name: "notes"}).Run(cfg)
	})
	if runErr != nil {
		t.Fatalf("CollectionShowCmd.Run: %v", runErr)
	}
	for _, want := range []string{"Name:", "notes", "Type:", "parser", "Path:", "Pattern:", "Documents:", "Chunks:", "Embedded:", "Semantic:"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestCollectionShowCmd_Missing(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := collectionTestConfig(t, dbPath)
	db := openTestStore(t, dbPath)
	db.Close()

	err := (&cmd.CollectionShowCmd{Name: "missing"}).Run(cfg)
	if err == nil || !strings.Contains(err.Error(), `collection "missing" not found`) {
		t.Fatalf("show missing = %v, want not-found error", err)
	}
}

// newSemanticHTTPServer implements the optional semantic service contract
// (/health + /tag) in-process so the reindex --semantic-only command path can
// run a real backfill without a network dependency.
func newSemanticHTTPServer(tags []string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(semantic.Health{
			Status:  "ok",
			Version: "test",
			Models:  semantic.Models{LID: true, Ner: true, Keyphrase: true, Topic: true},
		})
	})
	mux.HandleFunc("/tag", func(w http.ResponseWriter, r *http.Request) {
		var req semantic.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := semantic.Response{
			Results:    make([]semantic.TagResult, 0, len(req.Chunks)),
			CorpusLang: "en",
		}
		for _, c := range req.Chunks {
			resp.Results = append(resp.Results, semantic.TagResult{ID: c.ID, Tags: tags})
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(mux)
	return server
}

func TestCollectionReindexCmd_SemanticOnlyIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "note.md")
	if err := os.WriteFile(mdPath, []byte("# Hello\n\nGo concurrency notes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newSemanticHTTPServer([]string{"golang", "concurrency"})
	defer srv.Close()

	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := collectionTestConfig(t, dbPath)
	cfg.Config.Semantic = config.SemanticConfig{Enabled: true, BaseURL: srv.URL}

	// Index the markdown collection keyword-only so the document starts
	// without any semantic state (markdown is not yet on the enrichment
	// matrix during sync; the backfill brings it in).
	db := openTestStore(t, dbPath)
	col, err := db.CreateCollection("notes", store.CollectionTypeMarkdown, tmpDir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := (&cmd.SyncCmd{Collection: "notes", NoEmbed: true}).Run(cfg); err != nil {
		t.Fatalf("seed sync: %v", err)
	}

	// Capture the pre-backfill state.
	db2 := openTestStore(t, dbPath)
	doc, err := db2.GetDocumentContext(context.Background(), col.ID, mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := db2.FastFields().Get(doc.ID, "tags"); v != nil {
		t.Fatalf("tags before backfill = %v, want none", v)
	}
	preChunks, _ := db2.CountChunks(col.ID)
	preFTS, err := db2.SearchFTSContext(context.Background(), "concurrency", 10, nil)
	if err != nil {
		t.Fatalf("pre FTS search: %v", err)
	}
	if len(preFTS) != 1 {
		t.Fatalf("pre FTS results = %d, want 1", len(preFTS))
	}
	srcBefore, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	db2.Close()

	out := captureStdout(t, func() {
		err = (&cmd.CollectionReindexCmd{Name: "notes", SemanticOnly: true}).Run(cfg)
	})
	if err != nil {
		t.Fatalf("CollectionReindexCmd.Run semantic-only: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "processed") {
		t.Errorf("reindex output should report processed/skipped/failed:\n%s", out)
	}

	// The backfill wrote the semantic fast fields and marked the doc current.
	db3 := openTestStore(t, dbPath)
	defer db3.Close()
	doc2, err := db3.GetDocumentContext(context.Background(), col.ID, mdPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := db3.FastFields().Get(doc2.ID, "tags")
	if err != nil {
		t.Fatal(err)
	}
	if got != "golang,concurrency" {
		t.Errorf("tags after backfill = %q, want golang,concurrency", got)
	}
	states, err := db3.GetSemanticStates(context.Background(), []int64{doc2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if st := states[doc2.ID]; st.Status != store.SemanticStatusCurrent {
		t.Errorf("semantic status = %q, want current", st.Status)
	}

	// Isolation: chunks, embedded chunks, and the FTS rowset are untouched.
	postChunks, _ := db3.CountChunks(col.ID)
	if postChunks != preChunks {
		t.Errorf("chunk count changed by backfill: %d -> %d", preChunks, postChunks)
	}
	details, err := db3.CollectionDetails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 || details[0].EmbeddedChunks != 0 {
		t.Errorf("embedded chunks changed by backfill: %+v", details)
	}
	postFTS, err := db3.SearchFTSContext(context.Background(), "concurrency", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(postFTS) != len(preFTS) || postFTS[0].Content != preFTS[0].Content ||
		postFTS[0].Path != preFTS[0].Path || postFTS[0].Title != preFTS[0].Title {
		t.Errorf("FTS search outcome changed by backfill:\n before: %+v\n after:  %+v", preFTS, postFTS)
	}
	srcAfter, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(srcAfter) != string(srcBefore) {
		t.Error("backfill modified the source file")
	}
}

func TestCollectionReindexCmd_UnknownCollection(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "seek.db")
	cfg := collectionTestConfig(t, dbPath)
	db := openTestStore(t, dbPath)
	db.Close()

	err := (&cmd.CollectionReindexCmd{Name: "missing"}).Run(cfg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("reindex missing = %v, want not-found error", err)
	}
}

// TestCollectionReindexCmd_ProfileMismatchHint pins the CLI error surface for
// the embedding-profile contract: a reindex against a different vector space
// must refuse and surface the reindex hint from the service error.
func TestCollectionReindexCmd_ProfileMismatchHint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Hello\n\nSome body."), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "seek.db")
	cfg := collectionTestConfig(t, dbPath)
	cfg.Config.Embedding = config.EmbeddingConfig{Model: "model-b", Dimensions: 768}

	rt, err := app.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	col, err := rt.Store.CreateCollection("notes", store.CollectionTypeMarkdown, dir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	// A prior index claimed a different vector space and actually embedded
	// chunks, so the mismatch is meaningful.
	old := store.EmbeddingProfile{
		ProviderKind:  "local",
		Model:         "model-a",
		Dimensions:    384,
		Normalization: store.EmbeddingNormalizationVersion,
	}
	if err := rt.Store.ClaimEmbeddingProfile(context.Background(), old); err != nil {
		t.Fatalf("claim old profile: %v", err)
	}
	docID, err := rt.Store.UpsertDocument(col.ID, filepath.Join(dir, "note.md"), "Hello", "h1", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Store.InsertChunk(docID, 0, "# Hello", []float32{0.1, 0.2, 0.3}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}

	// The default reindex refuses the mismatch; the CLI surface shows the
	// service's reindex hint instead of a bare error.
	err = (&cmd.CollectionReindexCmd{Name: "notes"}).Run(cfg)
	if err == nil {
		t.Fatal("reindex with a mismatched vector space should refuse")
	}
	for _, want := range []string{"embedding profile mismatch", "AllowVectorSpaceChange", "seek rm"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("profile-mismatch error missing hint %q: %v", want, err)
		}
	}
}
