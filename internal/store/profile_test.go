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
