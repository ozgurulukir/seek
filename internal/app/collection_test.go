package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/pipeline"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/semantic"
	"github.com/ozgurulukir/seek/internal/store"
)

// --- List / Show ---

func TestCollectionServiceListShow(t *testing.T) {
	cfg := &config.AppConfig{
		Config: config.Config{
			VectorIndex: config.VectorIndexConfig{Backend: "linear"},
			// Vectors in these tests are 3-dimensional; without an explicit
			// dimension the runtime defaults to 1024 and SyncVectorIndex fails
			// with a dimension mismatch.
			Embedding: config.EmbeddingConfig{Dimensions: 3},
		},
		DBPath: filepath.Join(t.TempDir(), "seek.db"),
	}
	rt, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close()

	col, err := rt.Store.CreateCollection("notes", store.CollectionTypeCode, "/repo", "**/*.go")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	docID, err := rt.Store.UpsertDocument(col.ID, "/repo/main.go", "main", "h", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if err := rt.Store.InsertChunk(docID, 0, "package main", []float32{1, 2, 3}); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}

	svc := NewCollectionService(rt)
	info, err := svc.Show(context.Background(), "notes")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if info.Type != store.CollectionTypeCode || info.Path != "/repo" || info.Pattern != "**/*.go" {
		t.Errorf("Show info = type %q path %q pattern %q", info.Type, info.Path, info.Pattern)
	}
	if info.Documents != 1 || info.Chunks != 1 || info.EmbeddedChunks != 1 {
		t.Errorf("Show counts = %d docs/%d chunks/%d embedded, want 1/1/1",
			info.Documents, info.Chunks, info.EmbeddedChunks)
	}

	if _, err := svc.Show(context.Background(), "missing"); err == nil {
		t.Error("Show of a missing collection should fail")
	}

	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Name != "notes" {
		t.Errorf("List = %+v, want one collection named notes", list)
	}
}

// --- Rename E2E ---

func TestCollectionRenameE2E(t *testing.T) {
	cfg := &config.AppConfig{
		Config: config.Config{
			VectorIndex: config.VectorIndexConfig{Backend: "linear"},
			// The test vector embedding below is 3-dimensional; without an
			// explicit dimension the runtime defaults to 1024 and
			// SyncVectorIndex fails with a dimension mismatch before any
			// rename code runs.
			Embedding: config.EmbeddingConfig{Dimensions: 3},
		},
		DBPath: filepath.Join(t.TempDir(), "seek.db"),
	}
	rt, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close()

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

	beforeDocs, _ := rt.Store.CountDocuments(col.ID)
	beforeChunks, _ := rt.Store.CountChunks(col.ID)

	svc := NewCollectionService(rt)
	if err := svc.Rename(context.Background(), "old", "new"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// BM25 search returns the new collection name.
	results, err := rt.Search.SearchBM25(context.Background(), "composition", 10, search.Options{})
	if err != nil {
		t.Fatalf("SearchBM25: %v", err)
	}
	if len(results) != 1 || results[0].Collection != "new" {
		t.Errorf("BM25 results = %+v, want one result with collection %q", results, "new")
	}

	// Old name is gone; new name resolves.
	if _, err := rt.Store.GetCollectionByName("old"); err == nil {
		t.Error("old collection name still resolves after rename")
	}
	if _, err := rt.Store.GetCollectionByName("new"); err != nil {
		t.Errorf("new collection name missing: %v", err)
	}

	// Document/chunk counts are unchanged.
	afterDocs, _ := rt.Store.CountDocuments(col.ID)
	afterChunks, _ := rt.Store.CountChunks(col.ID)
	if afterDocs != beforeDocs || afterChunks != beforeChunks {
		t.Errorf("counts changed by rename: %d/%d -> %d/%d",
			beforeDocs, beforeChunks, afterDocs, afterChunks)
	}

	// Vector search still finds the chunk and reports the new name — the
	// vector graph (keyed by chunk id) is untouched by the rename.
	vecResults, err := rt.Store.SearchVectorContext(context.Background(), embed, 10, nil)
	if err != nil {
		t.Fatalf("SearchVectorContext: %v", err)
	}
	if len(vecResults) != 1 || vecResults[0].Collection != "new" {
		t.Errorf("vector results = %+v, want one result with collection %q", vecResults, "new")
	}
}

// --- Path-scoped sync safety ---

func TestCollectionServiceSyncPathValidates(t *testing.T) {
	cfg := &config.AppConfig{
		Config: config.Config{VectorIndex: config.VectorIndexConfig{Backend: "linear"}},
		DBPath: filepath.Join(t.TempDir(), "seek.db"),
	}
	rt, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close()

	dir := t.TempDir()
	md := filepath.Join(dir, "note.md")
	if err := os.WriteFile(md, []byte("# Hello\n\nPlan body."), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Store.CreateCollection("notes", store.CollectionTypeMarkdown, dir, "**/*.md"); err != nil {
		t.Fatal(err)
	}

	svc := NewCollectionService(rt)
	discard := pipeline.NewStdoutLogger(io.Discard)

	// An outside path is rejected before any index work.
	outside := filepath.Join(t.TempDir(), "other.md")
	if err := os.WriteFile(outside, []byte("# outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncPath(context.Background(), "notes", outside, pipeline.Options{}, discard); err == nil {
		t.Error("sync of an outside path should be rejected")
	}

	// A path inside the collection dispatches to the markdown handler and
	// indexes the file.
	report, err := svc.SyncPath(context.Background(), "notes", md, pipeline.Options{SkipEmbed: true}, discard)
	if err != nil {
		t.Fatalf("SyncPath inside collection: %v", err)
	}
	if report.Indexed == 0 {
		t.Errorf("inside sync report = %+v, want indexed > 0", report)
	}
}

// TestCollectionServiceSyncPathIsGuardedNotScoped pins the accepted design
// (reviewer decision (b)): SyncPath validates canonical containment and then
// runs a WHOLE-COLLECTION sync — the path is a security guard, not a scope
// filter. With two files in the collection, syncing --path file1 must still
// index both files, distinguishing the guarded sync from a hypothetical
// path-scoped one (which would only index file1).
func TestCollectionServiceSyncPathIsGuardedNotScoped(t *testing.T) {
	cfg := &config.AppConfig{
		Config: config.Config{VectorIndex: config.VectorIndexConfig{Backend: "linear"}},
		DBPath: filepath.Join(t.TempDir(), "seek.db"),
	}
	rt, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close()

	dir := t.TempDir()
	file1 := filepath.Join(dir, "a.md")
	file2 := filepath.Join(dir, "b.md")
	for _, f := range []string{file1, file2} {
		if err := os.WriteFile(f, []byte("# Body\n\nContent of the note."), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	col, err := rt.Store.CreateCollection("notes", store.CollectionTypeMarkdown, dir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}

	svc := NewCollectionService(rt)
	discard := pipeline.NewStdoutLogger(io.Discard)

	// Syncing --path file1 indexes the WHOLE collection (both files).
	report, err := svc.SyncPath(context.Background(), "notes", file1, pipeline.Options{SkipEmbed: true}, discard)
	if err != nil {
		t.Fatalf("SyncPath guarded sync: %v", err)
	}
	if report.Indexed == 0 {
		t.Errorf("guard sync report = %+v, want indexed > 0", report)
	}
	docs, err := rt.Store.CountDocuments(col.ID)
	if err != nil {
		t.Fatal(err)
	}
	if docs != 2 {
		t.Fatalf("guarded SyncPath indexed %d docs, want 2 (whole collection, not just the validated path)", docs)
	}
	for _, want := range []string{file1, file2} {
		if _, err := rt.Store.GetDocumentContext(context.Background(), col.ID, want); err != nil {
			t.Errorf("document %q missing after guarded sync: %v", want, err)
		}
	}
}

func TestValidateCollectionPathOutOfBounds(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "sub", "note.md")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}

	if err := ValidateCollectionPath(col, inside); err != nil {
		t.Errorf("inside path rejected: %v", err)
	}
	if err := ValidateCollectionPath(col, root); err != nil {
		t.Errorf("collection root itself rejected: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(outside, []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCollectionPath(col, outside); err == nil {
		t.Error("outside path accepted")
	}

	// A parent-escape path is rejected even though the file exists.
	escape := filepath.Join(root, "..", "escape.md")
	if err := os.WriteFile(escape, []byte("# x"), 0o644); err == nil {
		defer os.Remove(escape)
		if err := ValidateCollectionPath(col, escape); err == nil {
			t.Error("parent-escape path accepted")
		}
	}

	// Ellipsis boundary: sibling directory with a shared prefix must be outside.
	sibling := root + "-sibling"
	if err := os.MkdirAll(sibling, 0o755); err == nil {
		defer os.RemoveAll(sibling)
		sibFile := filepath.Join(sibling, "note.md")
		if err := os.WriteFile(sibFile, []byte("# hi"), 0o644); err == nil {
			if err := ValidateCollectionPath(col, sibFile); err == nil {
				t.Error("sibling-with-prefix path accepted as inside")
			}
		}
	}
}

func TestValidateCollectionPathSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDir, "note.md"), []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outsideDir, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation requires privileges on Windows")
		}
		t.Fatalf("create symlink: %v", err)
	}
	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}
	if err := ValidateCollectionPath(col, filepath.Join(link, "note.md")); err == nil {
		t.Error("symlink escape accepted")
	}
}

// TestValidateCollectionPathDanglingSymlinkRejected pins the fail-closed fix:
// a dangling symlink (link entry exists, target does not) must be rejected,
// never accepted via lexical containment. EvalSymlinks fails on it, so the
// unresolved path must NOT be used as a fallback.
func TestValidateCollectionPathDanglingSymlinkRejected(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(filepath.Join(t.TempDir(), "nowhere"), link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation requires privileges on Windows")
		}
		t.Fatalf("create symlink: %v", err)
	}
	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}
	if err := ValidateCollectionPath(col, link); err == nil {
		t.Error("dangling symlink path accepted (must be rejected — fail closed)")
	}
}

// TestValidateCollectionPathNonExistingTargetAccepted verifies that a path
// which does not exist yet (which sync will create) is accepted and resolves
// to the correct canonical path: the resolved collection root joined with the
// remaining segments.
func TestValidateCollectionPathNonExistingTargetAccepted(t *testing.T) {
	root := t.TempDir()
	// A real file inside the collection so the ancestor chain resolves.
	if err := os.WriteFile(filepath.Join(root, "existing.md"), []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := filepath.Join(root, "sub", "new-note.md")

	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}
	if err := ValidateCollectionPath(col, future); err != nil {
		t.Fatalf("non-existent target under collection root rejected: %v", err)
	}

	// It must canonicalize to the resolved root joined with the remaining
	// segments (sub/new-note.md), not the unresolved abs.
	rootResolved, err := canonicalPath(root)
	if err != nil {
		t.Fatalf("canonicalPath(root): %v", err)
	}
	got, err := canonicalPath(future)
	if err != nil {
		t.Fatalf("canonicalPath(future): %v", err)
	}
	rel, err := filepath.Rel(root, future)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	want := filepath.Join(rootResolved, rel)
	if got != want {
		t.Errorf("canonicalPath(%q) = %q, want %q", future, got, want)
	}
}

func TestValidateCollectionPathWindowsCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive containment is Windows-specific")
	}
	root := t.TempDir()
	inside := filepath.Join(root, "Note.md")
	if err := os.WriteFile(inside, []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}
	mixed := filepath.Join(root, "note.MD")
	if err := ValidateCollectionPath(col, mixed); err != nil {
		t.Errorf("case-mismatched path rejected on Windows: %v", err)
	}
}

// --- Reindex profile contract ---

func indexCfg(t *testing.T, model string, dims int) *config.AppConfig {
	t.Helper()
	return &config.AppConfig{
		Config: config.Config{
			VectorIndex: config.VectorIndexConfig{Backend: "linear"},
			Embedding:   config.EmbeddingConfig{Model: model, Dimensions: dims},
		},
		DBPath: filepath.Join(t.TempDir(), "seek.db"),
	}
}

func TestReindexProfileMismatchRefusesThenPreserves(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Hello\n\nSome body."), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := indexCfg(t, "model-b", 768)
	rt, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close()

	col, err := rt.Store.CreateCollection("notes", store.CollectionTypeMarkdown, dir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	// A prior index claimed a different vector space and actually embedded
	// chunks, so the index is non-empty and the mismatch is meaningful.
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

	svc := NewCollectionService(rt)
	discard := pipeline.NewStdoutLogger(io.Discard)

	// Default reindex refuses the mismatch with a reindex hint.
	_, err = svc.Reindex(context.Background(), "notes", ReindexOptions{}, discard)
	if !errors.Is(err, store.ErrProfileMismatch) {
		t.Fatalf("reindex mismatch = %v, want ErrProfileMismatch", err)
	}
	stored, err := rt.Store.GetEmbeddingProfile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Model != "model-a" || stored.Dimensions != 384 {
		t.Errorf("refused reindex changed the profile: %+v", stored)
	}

	// Without an embedding provider, opting in must fail without losing the old
	// vector space.
	res, err := svc.Reindex(context.Background(), "notes", ReindexOptions{AllowVectorSpaceChange: true}, discard)
	if err == nil {
		t.Fatal("reindex with unavailable embedding provider should fail")
	}
	if !res.VectorSpaceChanged {
		t.Error("VectorSpaceChanged = false after attempted reset")
	}
	stored, err = rt.Store.GetEmbeddingProfile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Model != "model-a" || stored.Dimensions != 384 {
		t.Errorf("failed reindex did not preserve the old profile: %+v", stored)
	}
}

func TestReindexRefusesWhenOtherCollectionsHoldEmbeddings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Hello\n\nSome body."), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := indexCfg(t, "model-b", 768)
	rt, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close()

	col, err := rt.Store.CreateCollection("notes", store.CollectionTypeMarkdown, dir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	other, err := rt.Store.CreateCollection("other", store.CollectionTypeMarkdown, t.TempDir(), "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	// A prior index claimed a different vector space and embedded chunks in
	// both collections, so clearing the store-global profile would orphan the
	// other collection's embeddings.
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
	otherDoc, err := rt.Store.UpsertDocument(other.ID, filepath.Join(other.Path, "note.md"), "Other", "h2", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Store.InsertChunk(otherDoc, 0, "other body", []float32{0.4, 0.5, 0.6}); err != nil {
		t.Fatal(err)
	}

	svc := NewCollectionService(rt)
	discard := pipeline.NewStdoutLogger(io.Discard)

	// Even opting into the vector-space change must refuse: the profile is
	// store-global and other collections would be left on the old vector space.
	_, err = svc.Reindex(context.Background(), "notes", ReindexOptions{AllowVectorSpaceChange: true}, discard)
	if !errors.Is(err, store.ErrProfileMismatch) {
		t.Fatalf("reindex with other collection embedded = %v, want ErrProfileMismatch", err)
	}
	stored, err := rt.Store.GetEmbeddingProfile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Model != "model-a" || stored.Dimensions != 384 {
		t.Errorf("refused reindex changed the store-wide profile: %+v", stored)
	}
}

func TestReindexProfileMatchDoesNotClear(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Hello\n\nSome body."), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := indexCfg(t, "model-a", 384)
	rt, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close()

	if _, err := rt.Store.CreateCollection("notes", store.CollectionTypeMarkdown, dir, "**/*.md"); err != nil {
		t.Fatal(err)
	}
	// The stored profile already matches the current config.
	if err := rt.Store.ClaimEmbeddingProfile(context.Background(), store.ProfileFromConfig(cfg)); err != nil {
		t.Fatalf("claim matching profile: %v", err)
	}

	svc := NewCollectionService(rt)
	discard := pipeline.NewStdoutLogger(io.Discard)
	res, err := svc.Reindex(context.Background(), "notes", ReindexOptions{}, discard)
	if err != nil {
		t.Fatalf("reindex with matching profile: %v", err)
	}
	if res.VectorSpaceChanged {
		t.Error("matching profile must not clear the vector space")
	}
	stored, err := rt.Store.GetEmbeddingProfile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil {
		t.Error("matching profile was cleared by reindex")
	}
}

func TestReindexNoProfileSkipsValidation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No embedding model configured: a keyword-only reindex must succeed even
	// though no profile exists (nothing to validate).
	cfg := &config.AppConfig{
		Config: config.Config{VectorIndex: config.VectorIndexConfig{Backend: "linear"}},
		DBPath: filepath.Join(t.TempDir(), "seek.db"),
	}
	rt, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close()

	if _, err := rt.Store.CreateCollection("notes", store.CollectionTypeMarkdown, dir, "**/*.md"); err != nil {
		t.Fatal(err)
	}
	svc := NewCollectionService(rt)
	discard := pipeline.NewStdoutLogger(io.Discard)
	res, err := svc.Reindex(context.Background(), "notes", ReindexOptions{}, discard)
	if err != nil {
		t.Fatalf("keyword-only reindex: %v", err)
	}
	if res.VectorSpaceChanged {
		t.Error("no-profile keyword-only reindex must not report a vector-space change")
	}
}

// TestReindexAllRecoversProfileMismatchSingle pins the --all recovery path for a
// single collection whose stored embedding profile mismatches the current
// config: ReindexAll with AllowVectorSpaceChange clears the store-global profile
// and re-embeds. Without the flag it refuses, because the vector-space change is
// a deliberate action.
func TestReindexAllRecoversProfileMismatchSingle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Hello\n\nSome body."), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := indexCfg(t, "model-b", 768)
	rt, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close()

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

	svc := NewCollectionService(rt)
	discard := pipeline.NewStdoutLogger(io.Discard)

	// Without --allow-vector-space-change the store-wide recovery refuses:
	// changing the vector space is a deliberate action.
	_, err = svc.ReindexAll(context.Background(), ReindexOptions{}, discard)
	if err == nil || !strings.Contains(err.Error(), "--allow-vector-space-change") {
		t.Fatalf("ReindexAll without allow-change = %v, want rejection", err)
	}

	// With the flag but no embedding provider, it fails and preserves the old
	// store-wide vector space.
	results, err := svc.ReindexAll(context.Background(), ReindexOptions{AllowVectorSpaceChange: true}, discard)
	if err == nil {
		t.Fatal("ReindexAll with unavailable embedding provider should fail")
	}
	if len(results) != 1 {
		t.Fatalf("ReindexAll results = %d, want 1", len(results))
	}
	if !results[0].VectorSpaceChanged {
		t.Error("VectorSpaceChanged = false after clearing the profile")
	}
	stored, err := rt.Store.GetEmbeddingProfile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Model != "model-a" || stored.Dimensions != 384 {
		t.Errorf("failed ReindexAll did not preserve the old profile: %+v", stored)
	}
}

// TestReindexAllRecoversAcrossCollections pins the --all recovery path across
// multiple collections: when every collection holds chunks in a stored vector
// space that mismatches the current config, the collection-scoped Reindex still
// refuses (the store-global guard) but ReindexAll clears the profile and
// re-embeds them all.
func TestReindexAllRecoversAcrossCollections(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Hello\n\nSome body."), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := indexCfg(t, "model-b", 768)
	rt, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close()

	col, err := rt.Store.CreateCollection("notes", store.CollectionTypeMarkdown, dir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	otherDir := t.TempDir()
	other, err := rt.Store.CreateCollection("other", store.CollectionTypeMarkdown, otherDir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	// A prior index claimed a different vector space and embedded chunks in
	// both collections, so clearing the store-global profile would orphan the
	// other collection's embeddings.
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
	otherDoc, err := rt.Store.UpsertDocument(other.ID, filepath.Join(otherDir, "note.md"), "Other", "h2", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Store.InsertChunk(otherDoc, 0, "other body", []float32{0.4, 0.5, 0.6}); err != nil {
		t.Fatal(err)
	}

	svc := NewCollectionService(rt)
	discard := pipeline.NewStdoutLogger(io.Discard)

	// Collection-scoped reindex still refuses even with allow-change: the
	// store-global profile would orphan the other collection.
	_, err = svc.Reindex(context.Background(), "notes", ReindexOptions{AllowVectorSpaceChange: true}, discard)
	if !errors.Is(err, store.ErrProfileMismatch) {
		t.Fatalf("collection-scoped reindex with other embedded = %v, want ErrProfileMismatch", err)
	}

	// --all without an embedding provider fails and restores all old vectors.
	results, err := svc.ReindexAll(context.Background(), ReindexOptions{AllowVectorSpaceChange: true}, discard)
	if err == nil {
		t.Fatal("ReindexAll with unavailable embedding provider should fail")
	}
	if len(results) != 2 {
		t.Fatalf("ReindexAll results = %d, want 2", len(results))
	}
	var changed int
	for _, r := range results {
		if r.VectorSpaceChanged {
			changed++
		}
	}
	if changed == 0 {
		t.Error("no collection reported a vector-space change")
	}
	stored, err := rt.Store.GetEmbeddingProfile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Model != "model-a" || stored.Dimensions != 384 {
		t.Errorf("failed ReindexAll did not preserve the old profile: %+v", stored)
	}
}

// --- Semantic backfill (S3) ---

// fakeSemanticProvider is a minimal semantic.Provider test double for the
// app layer: Health reports configurable models, Tag returns canned tags.
type fakeSemanticProvider struct {
	models semantic.Models
	tags   []string
	tagErr error
}

func (f *fakeSemanticProvider) Health(ctx context.Context) (semantic.Health, error) {
	return semantic.Health{Status: "ok", Models: f.models}, nil
}

func (f *fakeSemanticProvider) Tag(ctx context.Context, req semantic.Request) (semantic.Response, error) {
	if f.tagErr != nil {
		return semantic.Response{}, f.tagErr
	}
	out := semantic.Response{Results: make([]semantic.TagResult, 0, len(req.Chunks))}
	for _, c := range req.Chunks {
		out.Results = append(out.Results, semantic.TagResult{ID: c.ID, Tags: f.tags})
	}
	out.CorpusLang = "en"
	return out, nil
}

// TestCollectionServiceBackfill runs the full service path: index a markdown
// collection with semantic enrichment off, then backfill via the
// CollectionService and verify the tags fast field is written and the
// semantic status is current.
func TestCollectionServiceBackfill(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Hello\n\nGo concurrency notes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.AppConfig{
		Config: config.Config{VectorIndex: config.VectorIndexConfig{Backend: "linear"}},
		DBPath: filepath.Join(t.TempDir(), "seek.db"),
	}
	rt, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close()

	col, err := rt.Store.CreateCollection("notes", store.CollectionTypeMarkdown, dir, "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	// Keyword-only sync: no embeddings configured, so the pass only indexes.
	discard := pipeline.NewStdoutLogger(io.Discard)
	if _, err := rt.Pipeline.Sync(context.Background(), col, pipeline.Options{SkipEmbed: true}, discard); err != nil {
		t.Fatalf("sync: %v", err)
	}

	doc, err := rt.Store.GetDocumentContext(context.Background(), col.ID, filepath.Join(dir, "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := rt.Store.FastFields().Get(doc.ID, "tags"); v != nil {
		t.Fatalf("tags before backfill = %v, want none", v)
	}

	// Inject a fake semantic provider into the runtime indexer used by the
	// service, mirroring how a real service would be resolved. pipeline.Logger
	// satisfies indexer.Logger (identical Printf signature), so `discard` is
	// reusable here.
	fake := &fakeSemanticProvider{
		models: semantic.Models{LID: true, Ner: true, Keyphrase: true, Topic: true},
		tags:   []string{"golang", "concurrency"},
	}
	rt.Indexer = indexer.NewWithDependencies(cfg, rt.Store, indexer.NewConfigExtractorResolver(cfg), rt.Store).
		WithLogger(discard).
		WithSemanticProvider(fake)

	svc := NewCollectionService(rt)
	report, err := svc.Backfill(context.Background(), "notes", discard)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if report.Processed != 1 || report.Failed != 0 || report.Skipped != 0 {
		t.Fatalf("backfill report = %+v, want 1 processed", report)
	}

	got, err := rt.Store.FastFields().Get(doc.ID, "tags")
	if err != nil || got != "golang,concurrency" {
		t.Fatalf("tags after backfill = %v (%v), want golang,concurrency", got, err)
	}
	states, err := rt.Store.GetSemanticStates(context.Background(), []int64{doc.ID})
	if err != nil {
		t.Fatal(err)
	}
	if st := states[doc.ID]; st.Fingerprint == "" || st.Status != store.SemanticStatusCurrent {
		t.Fatalf("semantic state = %+v, want current", st)
	}

	// Missing collection fails cleanly.
	if _, err := svc.Backfill(context.Background(), "missing", discard); err == nil {
		t.Fatal("Backfill of a missing collection should fail")
	}
}
