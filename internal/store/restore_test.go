package store

import (
	"context"
	"testing"
)

// T5 (audit 2026-11-21): pin the restore-path invariants that the P1
// spool-to-temp-file redesign of BackupEmbeddingSpace will need to preserve.
// The existing TestRestoreEmbeddingSpaceRestoresFTSContent covers FTS/image/
// fast-field/chunk revert; these tests add the profile row, the rollback
// invariant, the empty-backup full clear, and the compatible-index fallback.

func TestRestoreEmbeddingSpaceRestoresProfileRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	original := testProfile("model-a", 3)
	if err := s.ClaimEmbeddingProfile(ctx, original); err != nil {
		t.Fatalf("claim original: %v", err)
	}
	insertVecDoc(t, s, "doc", [][]float32{{0.1, 0.2, 0.3}})
	backup, err := s.BackupEmbeddingSpace(ctx)
	if err != nil {
		t.Fatalf("BackupEmbeddingSpace: %v", err)
	}

	// Move the store to a different vector space (destructive reindex path).
	if err := s.ResetEmbeddingSpaceForProfile(ctx, testProfile("model-b", 5)); err != nil {
		t.Fatalf("switch profile: %v", err)
	}

	if err := s.RestoreEmbeddingSpace(ctx, backup); err != nil {
		t.Fatalf("RestoreEmbeddingSpace: %v", err)
	}
	stored, err := s.GetEmbeddingProfile(ctx)
	if err != nil {
		t.Fatalf("GetEmbeddingProfile: %v", err)
	}
	if stored == nil || stored.Fingerprint != original.ComputeFingerprint() {
		t.Errorf("profile after restore = %v, want original %s", stored, original.ComputeFingerprint())
	}
}

func TestRestoreEmbeddingSpaceRollsBackOnChunkFailure(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.ClaimEmbeddingProfile(ctx, testProfile("model-a", 3)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	insertVecDoc(t, s, "doc", [][]float32{{0.1, 0.2, 0.3}})
	var docID int64
	if err := s.db.QueryRow(`SELECT id FROM documents LIMIT 1`).Scan(&docID); err != nil {
		t.Fatalf("document id: %v", err)
	}
	backup, err := s.BackupEmbeddingSpace(ctx)
	if err != nil {
		t.Fatalf("BackupEmbeddingSpace: %v", err)
	}

	// Mutate the store after the backup, then craft a corrupted backup whose
	// chunk references a document absent from backup.documents: the FK on
	// chunks fails the chunk re-insert and the whole restore must roll back,
	// leaving the post-backup mutations in place.
	if _, err := s.db.ExecContext(ctx, `UPDATE chunks SET content = 'mutated' WHERE document_id = ?`, docID); err != nil {
		t.Fatalf("mutate chunk: %v", err)
	}
	corrupt := *backup
	corrupt.chunks = append([]embeddingChunkBackup(nil), backup.chunks...)
	corrupt.chunks = append(corrupt.chunks, embeddingChunkBackup{documentID: docID + 100, seq: 0})
	if err := s.RestoreEmbeddingSpace(ctx, &corrupt); err == nil {
		t.Fatal("RestoreEmbeddingSpace succeeded with an FK-violating backup")
	}
	var content string
	if err := s.db.QueryRowContext(ctx, `SELECT content FROM chunks WHERE document_id = ?`, docID).Scan(&content); err != nil {
		t.Fatalf("read chunk: %v", err)
	}
	if content != "mutated" {
		t.Errorf("chunk content = %q, want post-backup mutation (restore must not have applied)", content)
	}
}

func TestRestoreEmbeddingSpaceEmptyBackupClearsAll(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.ClaimEmbeddingProfile(ctx, testProfile("model-a", 3)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	insertVecDoc(t, s, "doc", [][]float32{{0.1, 0.2, 0.3}})
	s.SetVectorIndex(newLinearIndex(3))
	if err := s.SyncVectorIndexContext(ctx); err != nil {
		t.Fatalf("SyncVectorIndexContext: %v", err)
	}

	// A nil backup is a no-op.
	if err := s.RestoreEmbeddingSpace(ctx, nil); err != nil {
		t.Fatalf("RestoreEmbeddingSpace(nil): %v", err)
	}
	if s.vector().Len() != 1 {
		t.Fatalf("nil backup must be a no-op, vector len = %d", s.vector().Len())
	}

	// An empty (not nil) backup represents "the old space had nothing": the
	// restore clears all documents and the profile row.
	if err := s.RestoreEmbeddingSpace(ctx, &EmbeddingSpaceBackup{}); err != nil {
		t.Fatalf("RestoreEmbeddingSpace(empty): %v", err)
	}
	var docCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&docCount); err != nil {
		t.Fatalf("count documents: %v", err)
	}
	if docCount != 0 {
		t.Errorf("documents after empty restore = %d, want 0", docCount)
	}
	if stored, err := s.GetEmbeddingProfile(ctx); err != nil || stored != nil {
		t.Errorf("profile after empty restore = %v (err %v), want nil", stored, err)
	}
}

func TestRestoreEmbeddingSpaceCompatibleIndexFallback(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	oldProfile := testProfile("model-a", 5)
	if err := s.ClaimEmbeddingProfile(ctx, oldProfile); err != nil {
		t.Fatalf("claim old: %v", err)
	}
	insertVecDoc(t, s, "doc5", [][]float32{{0.1, 0.2, 0.3, 0.4, 0.5}})
	backup, err := s.BackupEmbeddingSpace(ctx)
	if err != nil {
		t.Fatalf("BackupEmbeddingSpace: %v", err)
	}

	// Move to a different vector space with a narrower index, and make the
	// index non-trivially populated so the first post-restore sync fails on
	// the dimension mismatch and the compatible-index fallback must build a
	// replacement index for the restored profile.
	if err := s.ResetEmbeddingSpaceForProfile(ctx, testProfile("model-b", 3)); err != nil {
		t.Fatalf("ResetEmbeddingSpaceForProfile: %v", err)
	}
	insertVecDoc(t, s, "doc3", [][]float32{{0.1, 0.2, 0.3}})
	s.SetVectorIndex(newLinearIndex(3))
	if err := s.SyncVectorIndexContext(ctx); err != nil {
		t.Fatalf("sync narrow index: %v", err)
	}

	if err := s.RestoreEmbeddingSpace(ctx, backup); err != nil {
		t.Fatalf("RestoreEmbeddingSpace: %v", err)
	}
	// The active index must now match the restored 5-dimensional space.
	if err := s.vector().Add(999, []float32{0.1, 0.2, 0.3, 0.4, 0.5}); err != nil {
		t.Errorf("restored index rejected the 5-dim space: %v", err)
	}
	if err := s.vector().Add(998, []float32{0.1, 0.2, 0.3}); err == nil {
		t.Error("restored index accepted the old 3-dim space")
	}
	stored, err := s.GetEmbeddingProfile(ctx)
	if err != nil || stored == nil || stored.Fingerprint != oldProfile.ComputeFingerprint() {
		t.Errorf("profile after fallback restore = %v (err %v), want model-a/5", stored, err)
	}
}
