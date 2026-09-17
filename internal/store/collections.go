package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Collection CRUD. Extracted from store.go as part of the god-object
// decomposition — a mechanical move, no changes.

// ErrCollectionExists is returned when a rename target name is already taken.
var ErrCollectionExists = errors.New("collection name already exists")

// --- Collections ---

func (s *Store) CreateCollection(name string, typ CollectionType, path, pattern string) (*Collection, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO collections (name, type, path, pattern, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		name, typ, path, pattern, now, now,
	)
	if err != nil {
		return nil, err
	}
	// LastInsertId is unreliable with some SQLite/driver paths; fall back to a lookup.
	id, _ := res.LastInsertId()
	if id == 0 {
		if err := s.db.QueryRow(`SELECT id FROM collections WHERE name = ?`, name).Scan(&id); err != nil {
			return nil, err
		}
	}
	return &Collection{ID: id, Name: name, Type: typ, Path: path, Pattern: pattern}, nil
}

// CreateCollectionWithBackend is CreateCollection with a per-collection
// extractor backend override. The backend is persisted so subsequent syncs
// reconstruct the right extractor without relying on the global config default
// (which may differ from what was used at add time). Empty backend means "use
// the config default" and is what plain CreateCollection records implicitly.
func (s *Store) CreateCollectionWithBackend(name string, typ CollectionType, path, pattern, backend string) (*Collection, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO collections (name, type, path, pattern, backend, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, typ, path, pattern, backend, now, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		if err := s.db.QueryRow(`SELECT id FROM collections WHERE name = ?`, name).Scan(&id); err != nil {
			return nil, err
		}
	}
	return &Collection{ID: id, Name: name, Type: typ, Path: path, Pattern: pattern, Backend: backend}, nil
}

// CreateParserCollection creates a "parser" collection referencing a schema-driven parser.
func (s *Store) CreateParserCollection(name, path, pattern, parserName string) (*Collection, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO collections (name, type, path, pattern, parser_name, parser_version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		name, CollectionTypeParser, path, pattern, parserName, 0, now, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		if err := s.db.QueryRow(`SELECT id FROM collections WHERE name = ?`, name).Scan(&id); err != nil {
			return nil, err
		}
	}
	return &Collection{ID: id, Name: name, Type: CollectionTypeParser, Path: path, Pattern: pattern, ParserName: parserName}, nil
}

// UpdateCollectionParserVersion sets the detected schema version for a parser collection.
func (s *Store) UpdateCollectionParserVersion(colID int64, version int) error {
	return s.UpdateCollectionParserVersionContext(context.Background(), colID, version)
}

func (s *Store) UpdateCollectionParserVersionContext(ctx context.Context, colID int64, version int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE collections SET parser_version = ? WHERE id = ?`, version, colID)
	return err
}

// MaxDocumentMtime returns the maximum mtime among documents in a collection,
// or zero if there are no documents. Used for incremental sync of parser collections.
func (s *Store) MaxDocumentMtime(collectionID int64) (float64, error) {
	return s.MaxDocumentMtimeContext(context.Background(), collectionID)
}

func (s *Store) MaxDocumentMtimeContext(ctx context.Context, collectionID int64) (float64, error) {
	var maxMtime sql.NullFloat64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(mtime) FROM documents WHERE collection_id = ?`, collectionID,
	).Scan(&maxMtime)
	if err != nil {
		return 0, err
	}
	if !maxMtime.Valid {
		return 0, nil
	}
	return maxMtime.Float64, nil
}

func (s *Store) GetCollectionByName(name string) (*Collection, error) {
	return s.repositories.collections.getByName(name)
}

func (s *Store) ListCollections() ([]Collection, error) {
	return s.repositories.collections.list()
}

// DeleteCollection removes a collection and all its documents, chunks, FTS
// entries, and fast fields in a single transaction. The foreign-key cascade
// only covers documents→chunks — fast_fields and documents_fts would survive
// a mid-delete crash — so the five DELETEs must succeed or none of them is
// applied: an interrupted `seek rm` must not leave a partially removed
// collection behind (review 2026-09-17 M3).
func (s *Store) DeleteCollection(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete fast_fields for documents in this collection.
	// The table is lazily created; ignore "no such table" errors.
	if _, err := tx.Exec(`DELETE FROM fast_fields WHERE doc_id IN (SELECT id FROM documents WHERE collection_id = ?)`, id); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete fast_fields: %w", err)
		}
	}
	// Delete FTS entries for documents in this collection
	if _, err := tx.Exec(`DELETE FROM documents_fts WHERE rowid IN (SELECT id FROM documents WHERE collection_id = ?)`, id); err != nil {
		return fmt.Errorf("delete fts: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM chunks WHERE document_id IN (SELECT id FROM documents WHERE collection_id = ?)`, id); err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM documents WHERE collection_id = ?`, id); err != nil {
		return fmt.Errorf("delete documents: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM collections WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	return tx.Commit()
}

// RenameCollection atomically renames a collection to a new unique name. Only
// the collections row (and its updated_at) changes: documents, chunks, FTS
// entries, and fast fields reference the collection by id, so their storage
// and counts are untouched — the HNSW graph is keyed by chunk id and is also
// unaffected.
//
// A rename to an existing name fails with ErrCollectionExists; a missing
// source name fails with a not-found error; renaming a collection to itself is
// a no-op. The transaction follows the store's Pattern A (BeginTx/Rollback/
// Commit).
func (s *Store) RenameCollection(ctx context.Context, oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("rename collection: names must not be empty")
	}
	if oldName == newName {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rename transaction: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM collections WHERE name = ?`, oldName).Scan(&exists); err != nil {
		return fmt.Errorf("check source collection: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("collection %q not found", oldName)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM collections WHERE name = ?`, newName).Scan(&exists); err != nil {
		return fmt.Errorf("check target collection: %w", err)
	}
	if exists > 0 {
		return fmt.Errorf("%w: %q", ErrCollectionExists, newName)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `UPDATE collections SET name = ?, updated_at = ? WHERE name = ?`, newName, now, oldName); err != nil {
		return fmt.Errorf("rename collection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rename transaction: %w", err)
	}
	return nil
}

// CollectionDetail is the per-collection accounting used by collection
// listing and show. It is produced by one batched GROUP BY query so commands
// never issue N+1 per-collection count queries.
type CollectionDetail struct {
	Collection
	Documents       int
	Chunks          int
	EmbeddedChunks  int
	SemanticCurrent int
	SemanticStale   int
	SemanticError   int
}

// CollectionDetails returns every collection with its document, chunk,
// embedded-chunk, and semantic-state counts in a single query.
func (s *Store) CollectionDetails(ctx context.Context) ([]CollectionDetail, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.name, c.type, c.path, c.pattern, c.parser_name, c.parser_version, c.backend,
		       COUNT(DISTINCT d.id) AS documents,
		       COUNT(ch.id) AS chunks,
		       COALESCE(SUM(CASE WHEN ch.embedding IS NOT NULL THEN 1 ELSE 0 END), 0) AS embedded,
		       COALESCE(COUNT(DISTINCT CASE WHEN d.semantic_status = 'current' THEN d.id END), 0) AS sem_current,
		       COALESCE(COUNT(DISTINCT CASE WHEN d.semantic_status = 'stale' THEN d.id END), 0) AS sem_stale,
		       COALESCE(COUNT(DISTINCT CASE WHEN d.semantic_status = 'error' THEN d.id END), 0) AS sem_error
		FROM collections c
		LEFT JOIN documents d ON d.collection_id = c.id
		LEFT JOIN chunks ch ON ch.document_id = d.id
		GROUP BY c.id
		ORDER BY c.name`)
	if err != nil {
		return nil, fmt.Errorf("collection details: %w", err)
	}
	defer rows.Close()

	var details []CollectionDetail
	for rows.Next() {
		var d CollectionDetail
		var parserName, backend sql.NullString
		if err := rows.Scan(&d.ID, &d.Name, &d.Type, &d.Path, &d.Pattern,
			&parserName, &d.ParserVersion, &backend,
			&d.Documents, &d.Chunks, &d.EmbeddedChunks,
			&d.SemanticCurrent, &d.SemanticStale, &d.SemanticError); err != nil {
			return nil, fmt.Errorf("scan collection detail: %w", err)
		}
		d.ParserName = parserName.String
		d.Backend = backend.String
		details = append(details, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("collection details rows: %w", err)
	}
	return details, nil
}
