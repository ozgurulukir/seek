package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Document CRUD. Extracted from store.go as part of the god-object
// decomposition — a mechanical move, no changes.

// --- Documents ---

func (s *Store) GetDocument(collectionID int64, path string) (*Document, error) {
	return s.GetDocumentContext(context.Background(), collectionID, path)
}

func (s *Store) GetDocumentContext(ctx context.Context, collectionID int64, path string) (*Document, error) {
	return s.repositories.documents.getContext(ctx, collectionID, path)
}

func (s *Store) UpsertDocument(collectionID int64, path, title, contentHash string, mtime float64, lineCount int) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var id int64
	err := s.db.QueryRow(
		`INSERT INTO documents (collection_id, path, title, content_hash, mtime, line_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(collection_id, path) DO UPDATE SET
		   title = excluded.title,
		   content_hash = excluded.content_hash,
		   mtime = excluded.mtime,
		   line_count = excluded.line_count,
		   updated_at = excluded.updated_at
		 RETURNING id`,
		collectionID, path, title, contentHash, mtime, lineCount, now, now,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ListDocumentPaths returns all document paths for a collection.
func (s *Store) ListDocumentPaths(collectionID int64) (map[string]int64, error) {
	return s.ListDocumentPathsContext(context.Background(), collectionID)
}

func (s *Store) ListDocumentPathsContext(ctx context.Context, collectionID int64) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, path FROM documents WHERE collection_id = ?`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]int64)
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		m[path] = id
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list document paths rows: %w", err)
	}
	return m, nil
}

// DeleteDocument removes a document and its chunks/FTS/fast_field entries.
// Each DELETE is individually atomic; foreign key cascades handle orphan
// cleanup if interrupted between statements.
func (s *Store) DeleteDocument(docID int64) error {
	return s.DeleteDocumentContext(context.Background(), docID)
}

// DeleteDocumentContext removes a document and all searchable projections in
// one transaction. This prevents an interrupted cleanup from leaving FTS,
// chunks, or fast fields out of sync with the document row.
func (s *Store) DeleteDocumentContext(ctx context.Context, docID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete document tx: %w", err)
	}
	defer tx.Rollback()

	// fast_fields table is lazily created; ignore "no such table" errors.
	if _, err := tx.ExecContext(ctx, `DELETE FROM fast_fields WHERE doc_id = ?`, docID); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete fast_fields: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM documents_fts WHERE rowid = ?`, docID); err != nil {
		return fmt.Errorf("delete fts entry: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, docID); err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, docID); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	return tx.Commit()
}

func (s *Store) UpdateDocumentMtime(docID int64, mtime float64) error {
	return s.UpdateDocumentMtimeContext(context.Background(), docID, mtime)
}

func (s *Store) UpdateDocumentMtimeContext(ctx context.Context, docID int64, mtime float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE documents SET mtime = ? WHERE id = ?`, mtime, docID)
	return err
}

func (s *Store) UpdateDocumentContentHash(docID int64, contentHash string) error {
	return s.UpdateDocumentContentHashContext(context.Background(), docID, contentHash)
}

func (s *Store) UpdateDocumentContentHashContext(ctx context.Context, docID int64, contentHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE documents SET content_hash = ? WHERE id = ?`, contentHash, docID)
	return err
}

// DeleteOrphansContext removes stale documents and their projections as one
// transaction. A nil livePaths map means the entire collection is stale.
func (s *Store) DeleteOrphansContext(ctx context.Context, collectionID int64, livePaths map[string]bool) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin orphan cleanup tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id, path FROM documents WHERE collection_id = ?`, collectionID)
	if err != nil {
		return 0, fmt.Errorf("list orphan documents: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan orphan document: %w", err)
		}
		if livePaths == nil || !livePaths[path] {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("list orphan documents rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close orphan documents rows: %w", err)
	}

	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM fast_fields WHERE doc_id = ?`, id); err != nil && !strings.Contains(err.Error(), "no such table") {
			return 0, fmt.Errorf("delete orphan fast_fields: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM documents_fts WHERE rowid = ?`, id); err != nil {
			return 0, fmt.Errorf("delete orphan fts: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, id); err != nil {
			return 0, fmt.Errorf("delete orphan chunks: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id); err != nil {
			return 0, fmt.Errorf("delete orphan document: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}
