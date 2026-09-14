package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	return fmt.Errorf("%w: stored %s != desired %s; reindex with: seek collection reindex --all --allow-vector-space-change",
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
	return fmt.Errorf("%w: index was built with %s but config now wants %s; reindex with: seek collection reindex --all --allow-vector-space-change",
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

// ResetEmbeddingSpace removes every stored embedding and the profile that
// describes its vector space. It preserves the current vector-index backend
// and dimension; callers changing the vector space should use
// ResetEmbeddingSpaceForProfile.
func (s *Store) ResetEmbeddingSpace(ctx context.Context) error {
	return s.resetEmbeddingSpace(ctx, nil)
}

// ResetEmbeddingSpaceForProfile resets the stored vector space and prepares a
// vector index compatible with desired before any new embeddings are written.
// The replacement is installed only after the database reset commits, while a
// compatible old index is prepared for transaction-failure recovery.
func (s *Store) ResetEmbeddingSpaceForProfile(ctx context.Context, desired EmbeddingProfile) error {
	return s.resetEmbeddingSpace(ctx, &desired)
}

func (s *Store) resetEmbeddingSpace(ctx context.Context, desired *EmbeddingProfile) error {
	current := s.vector()
	oldProfile, err := s.GetEmbeddingProfile(ctx)
	if err != nil {
		return fmt.Errorf("read current embedding profile: %w", err)
	}

	// Build both possible index replacements before clearing anything. This
	// makes index construction failures non-destructive and allows recovery to
	// use the old dimension if the SQLite reset fails.
	var replacement, oldReplacement VectorIndex
	if current != nil && desired != nil {
		replacement, err = replacementVectorIndex(current, desired)
		if err != nil {
			return fmt.Errorf("prepare replacement vector index: %w", err)
		}
	}
	if current != nil && desired != nil && oldProfile != nil {
		oldReplacement, err = replacementVectorIndex(current, oldProfile)
		if err != nil {
			return fmt.Errorf("prepare rollback vector index: %w", err)
		}
	}

	restoreVector := func() error {
		if current == nil {
			return nil
		}
		if oldReplacement != nil {
			s.SetVectorIndex(oldReplacement)
		}
		return s.SyncVectorIndexContext(ctx)
	}

	if current != nil {
		if err := current.Clear(); err != nil {
			return fmt.Errorf("clear vector index: %w", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		if restoreErr := restoreVector(); restoreErr != nil {
			return fmt.Errorf("begin embedding space reset: %w (restore vector index: %v)", err, restoreErr)
		}
		return fmt.Errorf("begin embedding space reset: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE chunks SET embedding = NULL`); err != nil {
		_ = tx.Rollback()
		if restoreErr := restoreVector(); restoreErr != nil {
			return fmt.Errorf("clear chunk embeddings: %w (restore vector index: %v)", err, restoreErr)
		}
		return fmt.Errorf("clear chunk embeddings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM embedding_profile WHERE id = 1`); err != nil {
		_ = tx.Rollback()
		if restoreErr := restoreVector(); restoreErr != nil {
			return fmt.Errorf("clear embedding profile: %w (restore vector index: %v)", err, restoreErr)
		}
		return fmt.Errorf("clear embedding profile: %w", err)
	}
	if err := tx.Commit(); err != nil {
		if restoreErr := restoreVector(); restoreErr != nil {
			return fmt.Errorf("commit embedding space reset: %w (restore vector index: %v)", err, restoreErr)
		}
		return fmt.Errorf("commit embedding space reset: %w", err)
	}
	if replacement != nil {
		s.SetVectorIndex(replacement)
	}
	return nil
}

// EmbeddingSpaceBackup is a recoverable snapshot used by collection reindex.
type EmbeddingSpaceBackup struct {
	profile    *EmbeddingProfile
	documents  map[int64]embeddingDocumentBackup
	chunks     []embeddingChunkBackup
	fastFields []fastFieldBackup
}

type embeddingDocumentBackup struct {
	collectionID        sql.NullInt64
	path                string
	title               sql.NullString
	ftsTitle            string
	ftsContent          string
	ftsPresent          bool
	contentHash         sql.NullString
	mtime               sql.NullFloat64
	lineCount           sql.NullInt64
	createdAt           sql.NullString
	updatedAt           sql.NullString
	metadata            sql.NullString
	semanticFingerprint sql.NullString
	semanticStatus      sql.NullString
	semanticBasis       sql.NullString
	semanticSourceHash  sql.NullString
}

type fastFieldBackup struct {
	docID      int64
	fieldName  string
	fieldValue sql.NullString
}

type embeddingChunkBackup struct {
	documentID  int64
	seq         int
	content     string
	contentZstd []byte
	embedding   []byte
	chunkType   int
	imagePath   sql.NullString
	startLine   int
	endLine     int
	createdAt   string
}

// backupEmbeddingSpace snapshots the old vector space before a destructive
// reindex. It is intentionally private: callers must restore it only when the
// reindex operation did not complete.
func (s *Store) BackupEmbeddingSpace(ctx context.Context) (*EmbeddingSpaceBackup, error) {
	profile, err := s.GetEmbeddingProfile(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.collection_id, d.path, d.title,
		f.title, f.content, d.content_hash, d.mtime, d.line_count, d.created_at, d.updated_at,
		d.metadata, d.semantic_fingerprint, d.semantic_status, d.semantic_basis, d.semantic_source_hash
		FROM documents d
		LEFT JOIN documents_fts f ON f.rowid = d.id
		ORDER BY d.id`)
	if err != nil {
		return nil, fmt.Errorf("snapshot embedding documents: %w", err)
	}
	defer rows.Close()
	documents := make(map[int64]embeddingDocumentBackup)
	for rows.Next() {
		var documentID int64
		var document embeddingDocumentBackup
		var ftsTitle, ftsContent sql.NullString
		if err := rows.Scan(&documentID, &document.collectionID, &document.path, &document.title,
			&ftsTitle, &ftsContent, &document.contentHash, &document.mtime, &document.lineCount,
			&document.createdAt, &document.updatedAt, &document.metadata, &document.semanticFingerprint,
			&document.semanticStatus, &document.semanticBasis, &document.semanticSourceHash); err != nil {
			return nil, fmt.Errorf("scan embedding document snapshot: %w", err)
		}
		document.ftsTitle = ftsTitle.String
		document.ftsContent = ftsContent.String
		document.ftsPresent = ftsTitle.Valid || ftsContent.Valid
		documents[documentID] = document
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read embedding snapshot: %w", err)
	}
	chunkRows, err := s.db.QueryContext(ctx, `SELECT document_id, seq, content, content_zstd, embedding, COALESCE(chunk_type, 0), image_path,
		COALESCE(start_line, 0), COALESCE(end_line, 0), created_at FROM chunks ORDER BY document_id, seq`)
	if err != nil {
		return nil, fmt.Errorf("snapshot embedding chunks: %w", err)
	}
	defer chunkRows.Close()
	var chunks []embeddingChunkBackup
	for chunkRows.Next() {
		var chunk embeddingChunkBackup
		if err := chunkRows.Scan(&chunk.documentID, &chunk.seq, &chunk.content, &chunk.contentZstd, &chunk.embedding, &chunk.chunkType, &chunk.imagePath, &chunk.startLine, &chunk.endLine, &chunk.createdAt); err != nil {
			return nil, fmt.Errorf("scan embedding chunk snapshot: %w", err)
		}
		chunk.contentZstd = append([]byte(nil), chunk.contentZstd...)
		chunk.embedding = append([]byte(nil), chunk.embedding...)
		chunks = append(chunks, chunk)
	}
	if err := chunkRows.Err(); err != nil {
		return nil, fmt.Errorf("read embedding chunk snapshot: %w", err)
	}
	fieldRows, err := s.db.QueryContext(ctx, `SELECT doc_id, field_name, field_value FROM fast_fields ORDER BY doc_id, field_name`)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) && !strings.Contains(err.Error(), "no such table") {
			return nil, fmt.Errorf("snapshot fast fields: %w", err)
		}
	}
	var fastFields []fastFieldBackup
	if fieldRows != nil {
		defer fieldRows.Close()
		for fieldRows.Next() {
			var field fastFieldBackup
			if err := fieldRows.Scan(&field.docID, &field.fieldName, &field.fieldValue); err != nil {
				return nil, fmt.Errorf("scan fast field snapshot: %w", err)
			}
			fastFields = append(fastFields, field)
		}
		if err := fieldRows.Err(); err != nil {
			return nil, fmt.Errorf("read fast field snapshot: %w", err)
		}
	}
	return &EmbeddingSpaceBackup{profile: profile, documents: documents, chunks: chunks, fastFields: fastFields}, nil
}

func (s *Store) RestoreEmbeddingSpace(ctx context.Context, backup *EmbeddingSpaceBackup) error {
	if backup == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin embedding space restore: %w", err)
	}
	defer tx.Rollback()
	if len(backup.documents) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM documents`); err != nil {
			return fmt.Errorf("clear documents for restore: %w", err)
		}
	} else {
		placeholders := make([]string, 0, len(backup.documents))
		args := make([]interface{}, 0, len(backup.documents))
		for documentID := range backup.documents {
			placeholders = append(placeholders, "?")
			args = append(args, documentID)
		}
		deleteQuery := `DELETE FROM documents WHERE id NOT IN (` + strings.Join(placeholders, ",") + `)`
		if _, err := tx.ExecContext(ctx, deleteQuery, args...); err != nil {
			return fmt.Errorf("remove documents absent from restore: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM fast_fields`); err != nil && !strings.Contains(err.Error(), "no such table") {
		return fmt.Errorf("clear fast fields for restore: %w", err)
	}
	for documentID, document := range backup.documents {
		if _, err := tx.ExecContext(ctx, `INSERT INTO documents
			(id, collection_id, path, title, content_hash, mtime, line_count, created_at, updated_at,
			 metadata, semantic_fingerprint, semantic_status, semantic_basis, semantic_source_hash)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET collection_id=excluded.collection_id, path=excluded.path,
			 title=excluded.title, content_hash=excluded.content_hash, mtime=excluded.mtime,
			 line_count=excluded.line_count, created_at=excluded.created_at, updated_at=excluded.updated_at,
			 metadata=excluded.metadata, semantic_fingerprint=excluded.semantic_fingerprint,
			 semantic_status=excluded.semantic_status, semantic_basis=excluded.semantic_basis,
			 semantic_source_hash=excluded.semantic_source_hash`,
			documentID, document.collectionID, document.path, document.title, document.contentHash,
			document.mtime, document.lineCount, document.createdAt, document.updatedAt, document.metadata,
			document.semanticFingerprint, document.semanticStatus, document.semanticBasis, document.semanticSourceHash); err != nil {
			return fmt.Errorf("restore document %d: %w", documentID, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, documentID); err != nil {
			return fmt.Errorf("clear chunks for restored document %d: %w", documentID, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM documents_fts WHERE rowid = ?`, documentID); err != nil {
			return fmt.Errorf("clear FTS entry for restored document %d: %w", documentID, err)
		}
		if document.ftsPresent {
			if _, err := tx.ExecContext(ctx, `INSERT INTO documents_fts (rowid, title, content) VALUES (?, ?, ?)`,
				documentID, document.ftsTitle, document.ftsContent); err != nil {
				return fmt.Errorf("restore FTS entry for document %d: %w", documentID, err)
			}
		}
	}
	for _, chunk := range backup.chunks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO chunks
			(document_id, seq, content, content_zstd, embedding, chunk_type, image_path, start_line, end_line, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, chunk.documentID, chunk.seq, chunk.content, chunk.contentZstd,
			chunk.embedding, chunk.chunkType, chunk.imagePath, chunk.startLine, chunk.endLine, chunk.createdAt); err != nil {
			return fmt.Errorf("restore chunk document %d sequence %d: %w", chunk.documentID, chunk.seq, err)
		}
	}
	for _, field := range backup.fastFields {
		if _, err := tx.ExecContext(ctx, `INSERT INTO fast_fields (doc_id, field_name, field_value) VALUES (?, ?, ?)`,
			field.docID, field.fieldName, field.fieldValue); err != nil {
			return fmt.Errorf("restore fast field %q for document %d: %w", field.fieldName, field.docID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM embedding_profile WHERE id = 1`); err != nil {
		return fmt.Errorf("clear profile for restore: %w", err)
	}
	if p := backup.profile; p != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO embedding_profile
			(id, provider_kind, model, dimensions, document_task_prefix, query_task_prefix,
			normalization, fingerprint, created_at, updated_at)
			VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ProviderKind, p.Model, p.Dimensions, p.DocumentTaskPrefix, p.QueryTaskPrefix,
			p.Normalization, p.Fingerprint, p.CreatedAt, p.UpdatedAt); err != nil {
			return fmt.Errorf("restore embedding profile: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit embedding space restore: %w", err)
	}
	if s.vector() != nil {
		if err := s.SyncVectorIndexContext(ctx); err != nil {
			if backup.profile == nil {
				return fmt.Errorf("rebuild vector index after restore: %w", err)
			}
			replacement, replacementErr := replacementVectorIndex(s.vector(), backup.profile)
			if replacementErr != nil {
				return fmt.Errorf("rebuild vector index after restore: %w; create compatible index: %v", err, replacementErr)
			}
			s.SetVectorIndex(replacement)
			if err := s.SyncVectorIndexContext(ctx); err != nil {
				return fmt.Errorf("rebuild compatible vector index after restore: %w", err)
			}
		}
	}
	return nil
}

func replacementVectorIndex(current VectorIndex, profile *EmbeddingProfile) (VectorIndex, error) {
	dimension := profile.Dimensions
	if dimension <= 0 {
		return nil, fmt.Errorf("restored embedding profile has invalid dimension %d", dimension)
	}
	switch index := current.(type) {
	case *hnswIndex:
		replacement, err := newHNSWIndex(dimension, index.m, index.efSearch)
		if err != nil {
			return nil, err
		}
		replacement.persistPath = index.persistPath
		replacement.configFingerprint = profile.ComputeFingerprint()
		return replacement, nil
	case *linearIndex:
		return newLinearIndex(dimension), nil
	default:
		return nil, fmt.Errorf("unsupported vector index type %T", current)
	}
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

// HasEmbeddedChunksExcept reports whether any chunk outside the given
// collection carries an embedding. It backs the collection-scoped reindex
// guard: the embedding profile is store-global, so clearing it to establish a
// new vector space is only safe when no OTHER collection holds chunks in the
// stored vector space.
func (s *Store) HasEmbeddedChunksExcept(ctx context.Context, collectionID int64) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM chunks ch
		JOIN documents d ON d.id = ch.document_id
		WHERE ch.embedding IS NOT NULL AND d.collection_id != ?`, collectionID).Scan(&count); err != nil {
		return false, fmt.Errorf("count embedded chunks outside collection: %w", err)
	}
	return count > 0, nil
}

func profileLabel(p *EmbeddingProfile) string {
	if p == nil {
		return "<none>"
	}
	return fmt.Sprintf("%s/%s/%d", p.ProviderKind, p.Model, p.Dimensions)
}
