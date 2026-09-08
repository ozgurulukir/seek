package store

import (
	"fmt"
	"strings"
	"time"
)

// Document CRUD. Extracted from store.go as part of the god-object
// decomposition — a mechanical move, no changes.

// --- Documents ---

func (s *Store) GetDocument(collectionID int64, path string) (*Document, error) {
	d := &Document{}
	err := s.db.QueryRow(
		`SELECT id, collection_id, path, title, content_hash, mtime, line_count, created_at, updated_at
		 FROM documents WHERE collection_id = ? AND path = ?`,
		collectionID, path,
	).Scan(&d.ID, &d.CollectionID, &d.Path, &d.Title, &d.ContentHash, &d.Mtime, &d.LineCount, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
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
	rows, err := s.db.Query(`SELECT id, path FROM documents WHERE collection_id = ?`, collectionID)
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
	// fast_fields table is lazily created; ignore "no such table" errors.
	if _, err := s.db.Exec(`DELETE FROM fast_fields WHERE doc_id = ?`, docID); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete fast_fields: %w", err)
		}
	}
	if _, err := s.db.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID); err != nil {
		return fmt.Errorf("delete fts entry: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM chunks WHERE document_id = ?`, docID); err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM documents WHERE id = ?`, docID); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	return nil
}

func (s *Store) UpdateDocumentMtime(docID int64, mtime float64) error {
	_, err := s.db.Exec(`UPDATE documents SET mtime = ? WHERE id = ?`, mtime, docID)
	return err
}
