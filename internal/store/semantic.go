package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// SemanticStatus is the persisted enrichment state of a single document.
// It is stored as a short string in the documents.semantic_status column.
type SemanticStatus string

const (
	// SemanticStatusNone means no enrichment has been recorded for the
	// document (the fingerprint column is NULL). It is the zero value.
	SemanticStatusNone SemanticStatus = ""
	// SemanticStatusCurrent means the stored fingerprint matches the desired
	// one; no re-enrichment is needed.
	SemanticStatusCurrent SemanticStatus = "current"
	// SemanticStatusStale means the stored fingerprint differs from the desired
	// one (service model, capability set, schema version, or content changed).
	SemanticStatusStale SemanticStatus = "stale"
	// SemanticStatusError means the last enrichment attempt failed (e.g. the
	// semantic service was down). Previously stored fast fields are preserved.
	SemanticStatusError SemanticStatus = "error"
)

// SemanticCapabilities is the set of enrichment capabilities a semantic
// service run produced. A capability is included in the fingerprint so a
// change in which capabilities are requested invalidates previously enriched
// documents.
type SemanticCapabilities struct {
	Language  bool
	NER       bool
	Keyphrase bool
	Topic     bool
}

// SemanticSchemaVersion identifies the enrichment fast-field schema. Bump it
// when the set or meaning of semantic fast fields changes so existing documents
// are treated as stale and re-enriched.
const SemanticSchemaVersion = "v1"

// SemanticFingerprint describes the enrichment identity of one document. It
// encodes the semantic service/model identity, the capability set, the
// enrichment schema version, and the source content hash (plan §3.2). Two
// documents are considered equally enriched only when all four agree.
type SemanticFingerprint struct {
	ServiceModel  string
	Capabilities  SemanticCapabilities
	SchemaVersion string
	ContentHash   string
}

// Compute derives the canonical fingerprint over the enrichment identity.
// The result is stored in documents.semantic_fingerprint.
func (f SemanticFingerprint) Compute() string {
	canonical, _ := json.Marshal(struct {
		ServiceModel  string               `json:"service_model"`
		Capabilities  SemanticCapabilities `json:"capabilities"`
		SchemaVersion string               `json:"schema_version"`
		ContentHash   string               `json:"content_hash"`
	}{
		ServiceModel:  f.ServiceModel,
		Capabilities:  f.Capabilities,
		SchemaVersion: f.SchemaVersion,
		ContentHash:   f.ContentHash,
	})
	h := sha256.Sum256(canonical)
	return hex.EncodeToString(h[:])
}

// SemanticState is the persisted enrichment state of one document.
type SemanticState struct {
	Fingerprint string
	Status      SemanticStatus
}

// SemanticDocumentState pairs a document with its persisted enrichment state.
type SemanticDocumentState struct {
	DocumentID  int64
	Fingerprint string
	Status      SemanticStatus
}

// GetStaleSemanticDocuments returns the documents in a collection that need
// semantic re-enrichment, selected by a single batched query (no N+1). A
// document is stale when any of these hold:
//
//   - identity/schema/capability changed: semantic_basis IS NULL OR semantic_basis != <desiredBasis>;
//   - the last attempt errored: semantic_status = 'error' (guaranteed retry);
//   - no source signal was recorded: semantic_source_hash IS NULL;
//   - the source content changed: semantic_source_hash != documents.content_hash.
//
// <desiredBasis> is the identity-only basis (service/capability/schema, no
// content). The content comparison reads documents.content_hash directly, so a
// content change is detected without reading any chunks — the coarse selection
// is one query, and the precise per-doc chunk comparison happens in the caller.
func (s *Store) GetStaleSemanticDocuments(ctx context.Context, collectionID int64, desiredBasis string) ([]SemanticDocumentState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, semantic_fingerprint, semantic_status
		FROM documents
		WHERE collection_id = ?
		  AND (semantic_basis IS NULL OR semantic_basis != ?
		       OR semantic_status = 'error'
		       OR semantic_source_hash IS NULL
		       OR semantic_source_hash != documents.content_hash)`,
		collectionID, desiredBasis)
	if err != nil {
		return nil, fmt.Errorf("select stale semantic documents: %w", err)
	}
	defer rows.Close()

	var states []SemanticDocumentState
	for rows.Next() {
		var st SemanticDocumentState
		var fp sql.NullString
		var status sql.NullString
		if err := rows.Scan(&st.DocumentID, &fp, &status); err != nil {
			return nil, fmt.Errorf("scan stale semantic document: %w", err)
		}
		if fp.Valid {
			st.Fingerprint = fp.String
		}
		if status.Valid {
			st.Status = SemanticStatus(status.String)
		}
		states = append(states, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stale semantic documents rows: %w", err)
	}
	return states, nil
}

// GetSemanticStates returns the persisted enrichment state for a set of
// document IDs in one batched query. Documents with no recorded enrichment are
// returned with an empty SemanticState (SemanticStatusNone).
func (s *Store) GetSemanticStates(ctx context.Context, docIDs []int64) (map[int64]SemanticState, error) {
	if len(docIDs) == 0 {
		return make(map[int64]SemanticState), nil
	}

	placeholders := make([]string, len(docIDs))
	args := make([]interface{}, len(docIDs))
	for i, id := range docIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, semantic_fingerprint, semantic_status
		FROM documents
		WHERE id IN (%s)`,
		strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select semantic states: %w", err)
	}
	defer rows.Close()

	result := make(map[int64]SemanticState, len(docIDs))
	for rows.Next() {
		var id int64
		var fp sql.NullString
		var status sql.NullString
		if err := rows.Scan(&id, &fp, &status); err != nil {
			return nil, fmt.Errorf("scan semantic state: %w", err)
		}
		st := SemanticState{}
		if fp.Valid {
			st.Fingerprint = fp.String
		}
		if status.Valid {
			st.Status = SemanticStatus(status.String)
		}
		result[id] = st
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("semantic states rows: %w", err)
	}
	return result, nil
}

// UpdateSemanticState atomically writes a document's semantic fast fields and
// its enrichment fingerprint/status in one transaction (Pattern A). This is the
// "atomic index replacement" step for semantic enrichment.
//
// A nil fastFields map preserves the previously stored fast fields — the
// enrichment produced nothing this pass (e.g. the semantic service is down) and
// must not destroy existing metadata. The status should reflect the outcome
// (e.g. SemanticStatusError on failure). A non-nil map (even empty) replaces
// the stored fast fields, so callers can explicitly drop stale metadata when it
// was actually recomputed to nothing.
//
// The three semantic columns are written together:
//
//   - semantic_fingerprint is the full fingerprint (identity + chunk-based
//     content hash) used by the precise per-doc skip check;
//   - semantic_basis is the identity-only fingerprint (no content) used as the
//     coarse stale-selection key;
//   - semantic_source_hash mirrors documents.content_hash so the stale query
//     detects a content change SQL-side without reading chunks.
func (s *Store) UpdateSemanticState(ctx context.Context, docID int64, fingerprint, basis, sourceHash string, status SemanticStatus, fastFields map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin semantic state transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE documents SET semantic_fingerprint = ?, semantic_basis = ?, semantic_source_hash = ?, semantic_status = ? WHERE id = ?`,
		fingerprint, basis, sourceHash, string(status), docID); err != nil {
		return fmt.Errorf("update semantic state: %w", err)
	}

	if fastFields != nil {
		if err := ensureFastFieldsTx(ctx, tx); err != nil {
			return fmt.Errorf("ensure fast fields: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM fast_fields WHERE doc_id = ?`, docID); err != nil {
			return fmt.Errorf("delete fast fields: %w", err)
		}
		for field, value := range fastFields {
			if value == "" {
				continue
			}
			encoded, err := encodeFastFieldValue(value)
			if err != nil {
				return fmt.Errorf("encode fast field %q: %w", field, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO fast_fields (doc_id, field_name, field_value) VALUES (?, ?, ?)`,
				docID, field, encoded); err != nil {
				return fmt.Errorf("write fast field %q: %w", field, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit semantic state transaction: %w", err)
	}
	return nil
}
