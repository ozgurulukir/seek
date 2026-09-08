package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Collection CRUD. Extracted from store.go as part of the god-object
// decomposition — a mechanical move, no changes.

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
	_, err := s.db.Exec(`UPDATE collections SET parser_version = ? WHERE id = ?`, version, colID)
	return err
}

// MaxDocumentMtime returns the maximum mtime among documents in a collection,
// or zero if there are no documents. Used for incremental sync of parser collections.
func (s *Store) MaxDocumentMtime(collectionID int64) (float64, error) {
	var maxMtime sql.NullFloat64
	err := s.db.QueryRow(
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
	c := &Collection{}
	var parserName, backend sql.NullString
	err := s.db.QueryRow(
		`SELECT id, name, type, path, pattern, parser_name, parser_version, backend, created_at, updated_at
		 FROM collections WHERE name = ?`, name,
	).Scan(&c.ID, &c.Name, &c.Type, &c.Path, &c.Pattern, &parserName, &c.ParserVersion, &backend, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.ParserName = parserName.String
	c.Backend = backend.String
	return c, nil
}

func (s *Store) ListCollections() ([]Collection, error) {
	rows, err := s.db.Query(`SELECT id, name, type, path, pattern, parser_name, parser_version, backend, created_at, updated_at FROM collections ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []Collection
	for rows.Next() {
		var c Collection
		var parserName, backend sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.Path, &c.Pattern, &parserName, &c.ParserVersion, &backend, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.ParserName = parserName.String
		c.Backend = backend.String
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list collections rows: %w", err)
	}
	return cols, nil
}

// DeleteCollection removes a collection and all its documents, chunks, FTS
// entries, and fast fields. Each DELETE is individually atomic; foreign key
// cascades (ON DELETE CASCADE) handle orphan cleanup if the process is
// interrupted between statements.
func (s *Store) DeleteCollection(id int64) error {
	// Delete fast_fields for documents in this collection.
	// The table is lazily created; ignore "no such table" errors.
	if _, err := s.db.Exec(`DELETE FROM fast_fields WHERE doc_id IN (SELECT id FROM documents WHERE collection_id = ?)`, id); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete fast_fields: %w", err)
		}
	}
	// Delete FTS entries for documents in this collection
	if _, err := s.db.Exec(`DELETE FROM documents_fts WHERE rowid IN (SELECT id FROM documents WHERE collection_id = ?)`, id); err != nil {
		return fmt.Errorf("delete fts: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM chunks WHERE document_id IN (SELECT id FROM documents WHERE collection_id = ?)`, id); err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM documents WHERE collection_id = ?`, id); err != nil {
		return fmt.Errorf("delete documents: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM collections WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	return nil
}
