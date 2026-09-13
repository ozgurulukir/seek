package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
)

// EmbeddingProfile describes the single active vector space persisted in
// SQLite. It is the source of truth for which model/dimensions/task-prefix
// produced the stored chunk embeddings; the HNSW manifest is a cache copy of
// this record.
//
// The provider kind is stored instead of the API key or the full URL, so a
// credential or endpoint-host change does not invalidate an otherwise
// identical vector space.
type EmbeddingProfile struct {
	ProviderKind       string
	Model              string
	Dimensions         int
	DocumentTaskPrefix string
	QueryTaskPrefix    string
	Normalization      string
	Fingerprint        string
	CreatedAt          string
	UpdatedAt          string
}

// EmbeddingNormalizationVersion identifies the vector normalization scheme.
// Bump it when normalization/versioning semantics change so existing vectors
// are treated as incompatible.
const EmbeddingNormalizationVersion = "v1"

// ErrProfileMismatch is returned when the stored embedding profile does not
// match the profile derived from the current config.
var ErrProfileMismatch = errors.New("embedding profile mismatch")

// ProfileStatus is the result of validating a desired profile against the
// stored one.
type ProfileStatus int

const (
	// ProfileNoProfile means no profile is stored yet.
	ProfileNoProfile ProfileStatus = iota
	// ProfileMatch means the stored profile matches the desired one.
	ProfileMatch
	// ProfileMismatch means a profile is stored but differs from the desired one.
	ProfileMismatch
)

// ProfileFromConfig derives the embedding profile for the current config. The
// provider kind comes from the embed capability contract; the task prefixes
// are the resolved (auto-detected) values actually applied to texts. The
// fingerprint is computed over the semantic identity only, so an endpoint-host
// or credential change does not invalidate an otherwise identical vector
// space.
func ProfileFromConfig(cfg *config.AppConfig) EmbeddingProfile {
	if cfg == nil {
		return EmbeddingProfile{}
	}
	q, d := cfg.Config.Embedding.TaskPrefixes()
	return EmbeddingProfile{
		ProviderKind:       embed.DetectProviderKind(cfg).String(),
		Model:              cfg.Config.Embedding.Model,
		Dimensions:         cfg.Config.Embedding.Dimensions,
		DocumentTaskPrefix: d,
		QueryTaskPrefix:    q,
		Normalization:      EmbeddingNormalizationVersion,
	}
}

// ComputeFingerprint derives the canonical fingerprint over the semantic
// vector-space identity. Endpoint host and API key are deliberately excluded
// so a provider URL or credential change does not invalidate an otherwise
// identical vector space.
func (p EmbeddingProfile) ComputeFingerprint() string {
	canonical, _ := json.Marshal(struct {
		ProviderKind       string `json:"provider_kind"`
		Model              string `json:"model"`
		Dimensions         int    `json:"dimensions"`
		DocumentTaskPrefix string `json:"document_task_prefix"`
		QueryTaskPrefix    string `json:"query_task_prefix"`
		Normalization      string `json:"normalization"`
	}{
		ProviderKind:       p.ProviderKind,
		Model:              p.Model,
		Dimensions:         p.Dimensions,
		DocumentTaskPrefix: p.DocumentTaskPrefix,
		QueryTaskPrefix:    p.QueryTaskPrefix,
		Normalization:      p.Normalization,
	})
	h := sha256.Sum256(canonical)
	return hex.EncodeToString(h[:])
}

// GetEmbeddingProfile returns the stored profile, or nil when none exists.
func (s *Store) GetEmbeddingProfile(ctx context.Context) (*EmbeddingProfile, error) {
	row := s.db.QueryRowContext(ctx, `SELECT provider_kind, model, dimensions,
		document_task_prefix, query_task_prefix, normalization, fingerprint, created_at, updated_at
		FROM embedding_profile WHERE id = 1`)
	var p EmbeddingProfile
	err := row.Scan(&p.ProviderKind, &p.Model, &p.Dimensions, &p.DocumentTaskPrefix,
		&p.QueryTaskPrefix, &p.Normalization, &p.Fingerprint, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read embedding profile: %w", err)
	}
	return &p, nil
}

// ValidateEmbeddingProfile compares the desired profile against the stored
// one. It returns ProfileNoProfile when none is stored, ProfileMatch when the
// fingerprints agree, and ProfileMismatch otherwise.
func (s *Store) ValidateEmbeddingProfile(ctx context.Context, desired EmbeddingProfile) (ProfileStatus, error) {
	stored, err := s.GetEmbeddingProfile(ctx)
	if err != nil {
		return ProfileNoProfile, err
	}
	if stored == nil {
		return ProfileNoProfile, nil
	}
	if stored.Fingerprint == desired.ComputeFingerprint() {
		return ProfileMatch, nil
	}
	return ProfileMismatch, nil
}

// SetDesiredEmbeddingProfile records the embedding profile derived from the
// current config. It is set by the composition root so vector search can fail
// fast when the persisted embeddings were produced by a different vector space.
// A nil value disables validation (the lightweight OpenStore path).
func (s *Store) SetDesiredEmbeddingProfile(p *EmbeddingProfile) {
	s.desiredProfile = p
}

// ClaimEmbeddingProfile atomically creates the profile if none exists. When a
// profile already exists it is left untouched; a fingerprint mismatch is
// returned so the caller can fail before writing any embedding. Callers must
// invoke this before the first chunk embedding write of a pass.
//
// A mismatch on an empty index (no embedded chunks) is allowed to overwrite the
// profile: the plan permits a config change to establish a new vector space
// when there is nothing to invalidate. A mismatch on a full index fails fast
// with the reindex path; data is never deleted automatically.
func (s *Store) ClaimEmbeddingProfile(ctx context.Context, desired EmbeddingProfile) error {
	desired.Fingerprint = desired.ComputeFingerprint()
	now := time.Now().UTC().Format(time.RFC3339)
	desired.CreatedAt = now
	desired.UpdatedAt = now
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO embedding_profile
		(id, provider_kind, model, dimensions, document_task_prefix, query_task_prefix,
		 normalization, fingerprint, created_at, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		desired.ProviderKind, desired.Model, desired.Dimensions, desired.DocumentTaskPrefix,
		desired.QueryTaskPrefix, desired.Normalization, desired.Fingerprint, desired.CreatedAt, desired.UpdatedAt)
	if err != nil {
		return fmt.Errorf("claim embedding profile: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows > 0 {
		return nil
	}
	// A profile already exists. It must match, or the index must be empty so a
	// config change may establish a new vector space.
	stored, err := s.GetEmbeddingProfile(ctx)
	if err != nil {
		return err
	}
	if stored != nil && stored.Fingerprint == desired.Fingerprint {
		return nil
	}
	has, err := s.HasEmbeddedChunks(ctx)
	if err != nil {
		return err
	}
	if !has {
		return s.overwriteEmbeddingProfile(ctx, desired)
	}
	return fmt.Errorf("%w: stored %s != desired %s; reindex with: seek rm <collection> && seek add && seek embed -f",
		ErrProfileMismatch, profileLabel(stored), profileLabel(&desired))
}

// overwriteEmbeddingProfile replaces the stored profile. It is only called on
// an empty index, where a config change may establish a new vector space.
func (s *Store) overwriteEmbeddingProfile(ctx context.Context, desired EmbeddingProfile) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE embedding_profile SET
		provider_kind = ?, model = ?, dimensions = ?, document_task_prefix = ?,
		query_task_prefix = ?, normalization = ?, fingerprint = ?, updated_at = ?
		WHERE id = 1`,
		desired.ProviderKind, desired.Model, desired.Dimensions, desired.DocumentTaskPrefix,
		desired.QueryTaskPrefix, desired.Normalization, desired.Fingerprint, desired.UpdatedAt); err != nil {
		return fmt.Errorf("overwrite embedding profile: %w", err)
	}
	return nil
}

// validateVectorProfile fails fast on vector search when the persisted
// embeddings were produced by a different vector space than the current config.
// An empty index (no embedded chunks) is allowed to diverge so a config change
// can establish a new vector space; data is never deleted automatically.
func (s *Store) validateVectorProfile(ctx context.Context) error {
	if s.desiredProfile == nil {
		return nil
	}
	stored, err := s.GetEmbeddingProfile(ctx)
	if err != nil {
		return err
	}
	if stored == nil || stored.Fingerprint == s.desiredProfile.ComputeFingerprint() {
		return nil
	}
	has, err := s.HasEmbeddedChunks(ctx)
	if err != nil {
		return err
	}
	if !has {
		return nil
	}
	return fmt.Errorf("%w: index was built with %s but config now wants %s; reindex with: seek rm <collection> && seek add && seek embed -f",
		ErrProfileMismatch, profileLabel(stored), profileLabel(s.desiredProfile))
}

// ClearEmbeddingProfile removes the stored profile. It never deletes chunk
// data; callers use it only when the index is empty or a reindex is intended.
func (s *Store) ClearEmbeddingProfile(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM embedding_profile WHERE id = 1`); err != nil {
		return fmt.Errorf("clear embedding profile: %w", err)
	}
	return nil
}

// HasEmbeddedChunks reports whether any chunk carries an embedding. It is used
// to distinguish an empty index (a config change may create a new profile)
// from a full index (a mismatch must fail fast).
func (s *Store) HasEmbeddedChunks(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks WHERE embedding IS NOT NULL`).Scan(&count); err != nil {
		return false, fmt.Errorf("count embedded chunks: %w", err)
	}
	return count > 0, nil
}

func profileLabel(p *EmbeddingProfile) string {
	if p == nil {
		return "<none>"
	}
	return fmt.Sprintf("%s/%s/%d", p.ProviderKind, p.Model, p.Dimensions)
}
