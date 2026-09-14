package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func testProfile(model string, dim int) EmbeddingProfile {
	return EmbeddingProfile{
		ProviderKind:  "local",
		Model:         model,
		Dimensions:    dim,
		Normalization: EmbeddingNormalizationVersion,
	}
}

func TestClaimEmbeddingProfile_Creates(t *testing.T) {
	s := newTestStore(t)
	desired := testProfile("model-a", 384)
	if err := s.ClaimEmbeddingProfile(context.Background(), desired); err != nil {
		t.Fatalf("ClaimEmbeddingProfile: %v", err)
	}
	stored, err := s.GetEmbeddingProfile(context.Background())
	if err != nil {
		t.Fatalf("GetEmbeddingProfile: %v", err)
	}
	if stored == nil {
		t.Fatal("expected a stored profile")
	}
	if stored.Fingerprint != desired.ComputeFingerprint() {
		t.Errorf("stored fingerprint = %s, want %s", stored.Fingerprint, desired.ComputeFingerprint())
	}
	if stored.Model != "model-a" || stored.Dimensions != 384 {
		t.Errorf("stored = %s/%d, want model-a/384", stored.Model, stored.Dimensions)
	}
}

func TestClaimEmbeddingProfile_MatchIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	desired := testProfile("model-a", 384)
	if err := s.ClaimEmbeddingProfile(context.Background(), desired); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := s.ClaimEmbeddingProfile(context.Background(), desired); err != nil {
		t.Fatalf("second claim (match) should be idempotent: %v", err)
	}
}

func TestClaimEmbeddingProfile_MismatchFullIndexFailsFast(t *testing.T) {
	s := newTestStore(t)
	if err := s.ClaimEmbeddingProfile(context.Background(), testProfile("model-a", 384)); err != nil {
		t.Fatalf("claim model-a: %v", err)
	}
	// Populate the index with embedded chunks so it is no longer empty.
	insertVecDoc(t, s, "doc", [][]float32{{0.1, 0.2, 0.3}})

	err := s.ClaimEmbeddingProfile(context.Background(), testProfile("model-b", 768))
	if !errors.Is(err, ErrProfileMismatch) {
		t.Fatalf("ClaimEmbeddingProfile mismatch = %v, want ErrProfileMismatch", err)
	}
	if err == nil || !strings.Contains(err.Error(), "reindex") {
		t.Errorf("mismatch error should state the reindex path, got: %v", err)
	}
}

func TestClaimEmbeddingProfile_MismatchEmptyIndexOverwrites(t *testing.T) {
	s := newTestStore(t)
	if err := s.ClaimEmbeddingProfile(context.Background(), testProfile("model-a", 384)); err != nil {
		t.Fatalf("claim model-a: %v", err)
	}
	// Empty index: a config change may establish a new vector space.
	if err := s.ClaimEmbeddingProfile(context.Background(), testProfile("model-b", 768)); err != nil {
		t.Fatalf("empty-index config change should overwrite the profile: %v", err)
	}
	stored, err := s.GetEmbeddingProfile(context.Background())
	if err != nil {
		t.Fatalf("GetEmbeddingProfile: %v", err)
	}
	if stored == nil || stored.Model != "model-b" || stored.Dimensions != 768 {
		t.Errorf("stored = %v, want model-b/768", stored)
	}
}

func TestValidateEmbeddingProfile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	status, err := s.ValidateEmbeddingProfile(ctx, testProfile("model-a", 384))
	if err != nil {
		t.Fatalf("ValidateEmbeddingProfile (no profile): %v", err)
	}
	if status != ProfileNoProfile {
		t.Errorf("no-profile status = %v, want ProfileNoProfile", status)
	}

	if err := s.ClaimEmbeddingProfile(ctx, testProfile("model-a", 384)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	status, err = s.ValidateEmbeddingProfile(ctx, testProfile("model-a", 384))
	if err != nil {
		t.Fatalf("ValidateEmbeddingProfile (match): %v", err)
	}
	if status != ProfileMatch {
		t.Errorf("match status = %v, want ProfileMatch", status)
	}

	status, err = s.ValidateEmbeddingProfile(ctx, testProfile("model-b", 768))
	if err != nil {
		t.Fatalf("ValidateEmbeddingProfile (mismatch): %v", err)
	}
	if status != ProfileMismatch {
		t.Errorf("mismatch status = %v, want ProfileMismatch", status)
	}
}

func TestClearEmbeddingProfile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.ClaimEmbeddingProfile(ctx, testProfile("model-a", 384)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := s.ClearEmbeddingProfile(ctx); err != nil {
		t.Fatalf("ClearEmbeddingProfile: %v", err)
	}
	stored, err := s.GetEmbeddingProfile(ctx)
	if err != nil {
		t.Fatalf("GetEmbeddingProfile: %v", err)
	}
	if stored != nil {
		t.Errorf("expected nil profile after clear, got %v", stored)
	}
}

func TestResetEmbeddingSpaceClearsChunksProfileAndVectorIndex(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.ClaimEmbeddingProfile(ctx, testProfile("model-a", 3)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	s.SetVectorIndex(newLinearIndex(3))
	insertVecDoc(t, s, "doc", [][]float32{{0.1, 0.2, 0.3}})
	if err := s.SyncVectorIndexContext(ctx); err != nil {
		t.Fatalf("SyncVectorIndexContext: %v", err)
	}
	if s.vector() == nil || s.vector().Len() != 1 {
		t.Fatalf("expected one vector before reset")
	}

	if err := s.ResetEmbeddingSpace(ctx); err != nil {
		t.Fatalf("ResetEmbeddingSpace: %v", err)
	}
	if has, err := s.HasEmbeddedChunks(ctx); err != nil {
		t.Fatalf("HasEmbeddedChunks: %v", err)
	} else if has {
		t.Error("expected all chunk embeddings to be cleared")
	}
	if stored, err := s.GetEmbeddingProfile(ctx); err != nil {
		t.Fatalf("GetEmbeddingProfile: %v", err)
	} else if stored != nil {
		t.Errorf("expected nil profile after reset, got %v", stored)
	}
	if got := s.vector().Len(); got != 0 {
		t.Errorf("vector index length after reset = %d, want 0", got)
	}
}

func TestResetEmbeddingSpaceForProfileRebuildsIndexDimension(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	oldProfile := testProfile("model-a", 3)
	if err := s.ClaimEmbeddingProfile(ctx, oldProfile); err != nil {
		t.Fatalf("claim: %v", err)
	}
	s.SetVectorIndex(newLinearIndex(3))
	insertVecDoc(t, s, "doc", [][]float32{{0.1, 0.2, 0.3}})
	if err := s.SyncVectorIndexContext(ctx); err != nil {
		t.Fatalf("initial SyncVectorIndexContext: %v", err)
	}

	newProfile := testProfile("model-b", 5)
	if err := s.ResetEmbeddingSpaceForProfile(ctx, newProfile); err != nil {
		t.Fatalf("ResetEmbeddingSpaceForProfile: %v", err)
	}
	if err := s.vector().Add(42, []float32{0.1, 0.2, 0.3, 0.4, 0.5}); err != nil {
		t.Fatalf("replacement index rejected new dimension: %v", err)
	}
	if err := s.vector().Add(43, []float32{0.1, 0.2, 0.3}); err == nil {
		t.Fatal("replacement index accepted old dimension")
	}
}

type clearFailVector struct {
	*linearIndex
}

func (clearFailVector) Clear() error {
	return errors.New("injected vector clear failure")
}

func TestResetEmbeddingSpacePreservesDatabaseWhenVectorClearFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	profile := testProfile("model-a", 3)
	if err := s.ClaimEmbeddingProfile(ctx, profile); err != nil {
		t.Fatalf("claim: %v", err)
	}
	s.SetVectorIndex(&clearFailVector{linearIndex: newLinearIndex(3)})
	insertVecDoc(t, s, "doc", [][]float32{{0.1, 0.2, 0.3}})

	if err := s.ResetEmbeddingSpace(ctx); err == nil {
		t.Fatal("ResetEmbeddingSpace succeeded despite vector clear failure")
	}
	if has, err := s.HasEmbeddedChunks(ctx); err != nil {
		t.Fatalf("HasEmbeddedChunks: %v", err)
	} else if !has {
		t.Error("reset removed embeddings after vector clear failure")
	}
	stored, err := s.GetEmbeddingProfile(ctx)
	if err != nil {
		t.Fatalf("GetEmbeddingProfile: %v", err)
	}
	if stored == nil || stored.Fingerprint != profile.ComputeFingerprint() {
		t.Errorf("profile after failed reset = %#v, want original profile", stored)
	}
}

func TestRestoreEmbeddingSpaceRestoresFTSContent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.ClaimEmbeddingProfile(ctx, testProfile("model-a", 3)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	s.SetVectorIndex(newLinearIndex(3))
	insertVecDoc(t, s, "doc", [][]float32{{0.1, 0.2, 0.3}})
	var documentID int64
	if err := s.db.QueryRow(`SELECT id FROM documents LIMIT 1`).Scan(&documentID); err != nil {
		t.Fatalf("document id: %v", err)
	}
	if err := s.UpsertFTS(documentID, "old title", "old searchable content"); err != nil {
		t.Fatalf("old FTS: %v", err)
	}
	var collectionID int64
	if err := s.db.QueryRowContext(ctx, `SELECT collection_id FROM documents WHERE id = ?`, documentID).Scan(&collectionID); err != nil {
		t.Fatalf("collection id: %v", err)
	}
	// This represents a legacy image-only document: nullable document fields
	// must survive a backup, and its fast fields are part of searchable state.
	var imageDocumentID int64
	if err := s.db.QueryRowContext(ctx, `INSERT INTO documents
		(collection_id, path, title, content_hash, mtime, line_count, created_at, updated_at,
		 metadata, semantic_fingerprint, semantic_status, semantic_basis, semantic_source_hash)
		VALUES (?, ?, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)
		RETURNING id`, collectionID, "image-only.png").Scan(&imageDocumentID); err != nil {
		t.Fatalf("insert legacy document: %v", err)
	}
	if err := s.UpsertFTS(imageDocumentID, "image title", "image searchable content"); err != nil {
		t.Fatalf("image FTS: %v", err)
	}
	if err := s.FastFields().Set(imageDocumentID, "topic", "legacy"); err != nil {
		t.Fatalf("image fast field: %v", err)
	}
	backup, err := s.BackupEmbeddingSpace(ctx)
	if err != nil {
		t.Fatalf("BackupEmbeddingSpace: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE chunks SET content = 'new content' WHERE document_id = ?`, documentID); err != nil {
		t.Fatalf("change chunk: %v", err)
	}
	if err := s.UpsertFTS(documentID, "new title", "new searchable content"); err != nil {
		t.Fatalf("new FTS: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, imageDocumentID); err != nil {
		t.Fatalf("delete image document: %v", err)
	}
	if err := s.RestoreEmbeddingSpace(ctx, backup); err != nil {
		t.Fatalf("RestoreEmbeddingSpace: %v", err)
	}
	var title, content string
	if err := s.db.QueryRowContext(ctx, `SELECT title, content FROM documents_fts WHERE rowid = ?`, documentID).Scan(&title, &content); err != nil {
		t.Fatalf("restored FTS: %v", err)
	}
	if title != "old title" || content != "old searchable content" {
		t.Fatalf("restored FTS = %q/%q, want old snapshot", title, content)
	}
	var restoredImageTitle, restoredImageContent string
	if err := s.db.QueryRowContext(ctx, `SELECT title, content FROM documents_fts WHERE rowid = ?`, imageDocumentID).
		Scan(&restoredImageTitle, &restoredImageContent); err != nil {
		t.Fatalf("restored image FTS: %v", err)
	}
	if restoredImageTitle != "image title" || restoredImageContent != "image searchable content" {
		t.Fatalf("restored image FTS = %q/%q", restoredImageTitle, restoredImageContent)
	}
	value, err := s.FastFields().Get(imageDocumentID, "topic")
	if err != nil {
		t.Fatalf("restored image fast field: %v", err)
	}
	if value != "legacy" {
		t.Fatalf("restored image fast field = %v, want legacy", value)
	}
}

func TestSearchVectorProfileMismatchFailsFast(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertVecDoc(t, s, "doc", [][]float32{{0.1, 0.2, 0.3}})

	// Desired profile matches the stored one: search succeeds.
	desired := testProfile("model-a", 384)
	if err := s.ClaimEmbeddingProfile(ctx, desired); err != nil {
		t.Fatalf("claim: %v", err)
	}
	s.SetDesiredEmbeddingProfile(&desired)
	if _, err := s.SearchVectorContext(ctx, []float32{0.1, 0.2, 0.3}, 10, nil); err != nil {
		t.Fatalf("search with matching profile should succeed: %v", err)
	}

	// Config changes to a different vector space: search fails fast.
	other := testProfile("model-b", 768)
	s.SetDesiredEmbeddingProfile(&other)
	_, err := s.SearchVectorContext(ctx, []float32{0.1, 0.2, 0.3}, 10, nil)
	if !errors.Is(err, ErrProfileMismatch) {
		t.Fatalf("search mismatch = %v, want ErrProfileMismatch", err)
	}
	if err == nil || !strings.Contains(err.Error(), "reindex") {
		t.Errorf("mismatch error should state the reindex path, got: %v", err)
	}
}

// TestRecoverVectorIndex_CrossValidatesManifestProfile covers the plan §3.4
// requirement that the HNSW manifest fingerprint is cross-validated against
// the SQLite embedding profile. A divergence is surfaced as a warning (never
// an auto-delete) pointing at the reindex path.
func TestRecoverVectorIndex_CrossValidatesManifestProfile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertVecDoc(t, s, "doc", [][]float32{{0.1, 0.2, 0.3}})
	if err := s.ClaimEmbeddingProfile(ctx, testProfile("model-a", 384)); err != nil {
		t.Fatalf("claim model-a: %v", err)
	}

	// Build an HNSW index whose manifest fingerprint differs from the profile.
	idx, err := newHNSWIndex(3, 16, 50)
	if err != nil {
		t.Fatal(err)
	}
	idx.configFingerprint = testProfile("model-b", 768).ComputeFingerprint()
	s.SetVectorIndex(idx)

	if err := s.RecoverVectorIndex(ctx); err != nil {
		t.Fatalf("RecoverVectorIndex: %v", err)
	}
	if w := idx.Warning(); !strings.Contains(w, "reindex") {
		t.Errorf("cross-validation warning should mention reindex, got: %q", w)
	}
}

func TestSearchVectorEmptyIndexAllowsConfigChange(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// No embedded chunks: a config change must not fail search.
	desired := testProfile("model-b", 768)
	if err := s.ClaimEmbeddingProfile(ctx, testProfile("model-a", 384)); err != nil {
		t.Fatalf("claim model-a: %v", err)
	}
	s.SetDesiredEmbeddingProfile(&desired)
	if _, err := s.SearchVectorContext(ctx, []float32{0.1, 0.2, 0.3}, 10, nil); err != nil {
		t.Fatalf("empty-index search with changed config should not fail: %v", err)
	}
}

// TestSearchVectorSucceedsAfterReindex covers the plan §5 integration path:
// after a mismatch fails fast, clearing the profile and re-claiming under the
// new config lets search succeed again.
func TestSearchVectorSucceedsAfterReindex(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertVecDoc(t, s, "doc", [][]float32{{0.1, 0.2, 0.3}})

	old := testProfile("model-a", 384)
	if err := s.ClaimEmbeddingProfile(ctx, old); err != nil {
		t.Fatalf("claim model-a: %v", err)
	}
	s.SetDesiredEmbeddingProfile(&old)
	if _, err := s.SearchVectorContext(ctx, []float32{0.1, 0.2, 0.3}, 10, nil); err != nil {
		t.Fatalf("search under model-a: %v", err)
	}

	// Config changes to model-b/768: search fails fast.
	next := testProfile("model-b", 768)
	s.SetDesiredEmbeddingProfile(&next)
	if _, err := s.SearchVectorContext(ctx, []float32{0.1, 0.2, 0.3}, 10, nil); !errors.Is(err, ErrProfileMismatch) {
		t.Fatalf("search under model-b = %v, want ErrProfileMismatch", err)
	}

	// Reindex: clear the profile, claim the new one, search succeeds.
	if err := s.ClearEmbeddingProfile(ctx); err != nil {
		t.Fatalf("ClearEmbeddingProfile: %v", err)
	}
	if err := s.ClaimEmbeddingProfile(ctx, next); err != nil {
		t.Fatalf("claim model-b after reindex: %v", err)
	}
	if _, err := s.SearchVectorContext(ctx, []float32{0.1, 0.2, 0.3}, 10, nil); err != nil {
		t.Fatalf("search after reindex should succeed: %v", err)
	}
}

func TestHasEmbeddedChunksExcept(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// No embedded chunks anywhere: nothing outside the target collection.
	before, err := s.HasEmbeddedChunksExcept(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if before {
		t.Error("HasEmbeddedChunksExcept = true on an empty store")
	}

	col, err := s.CreateCollection("target", CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := s.UpsertDocument(col.ID, "/tmp/a.md", "a", "h1", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertChunk(docID, 0, "a", []float32{1, 2}); err != nil {
		t.Fatal(err)
	}

	// The only embedded chunk belongs to the target collection.
	other, err := s.HasEmbeddedChunksExcept(ctx, col.ID)
	if err != nil {
		t.Fatal(err)
	}
	if other {
		t.Error("HasEmbeddedChunksExcept = true with only the target collection embedded")
	}

	// A second collection with an embedded chunk flips the guard.
	otherCol, err := s.CreateCollection("other", CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	otherDoc, err := s.UpsertDocument(otherCol.ID, "/tmp/b.md", "b", "h2", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertChunk(otherDoc, 0, "b", []float32{3, 4}); err != nil {
		t.Fatal(err)
	}
	other, err = s.HasEmbeddedChunksExcept(ctx, col.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !other {
		t.Error("HasEmbeddedChunksExcept = false despite an embedded chunk in another collection")
	}
	// Each collection sees the other's chunk as outside it: there is no
	// collection that owns every embedding anymore.
	other, err = s.HasEmbeddedChunksExcept(ctx, otherCol.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !other {
		t.Error("HasEmbeddedChunksExcept = false despite an embedded chunk outside the second collection")
	}
}
