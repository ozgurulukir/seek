package store

import (
	"context"
	"database/sql"
)

// The Store is the compatibility facade exposed to the rest of the
// application. These private repositories keep ownership of persistence
// concerns explicit without expanding the public API with one interface per
// table. New operations should be added to the narrow repository that owns
// them and surfaced through Store only when callers need them.
type storeRepositories struct {
	collections collectionRepository
	documents   documentRepository
	chunks      chunkRepository
	fts         ftsRepository
	fastFields  *FastFieldStore
	vectors     vectorRepository
}

func newStoreRepositories(db *sql.DB, fastFields *FastFieldStore) storeRepositories {
	return storeRepositories{
		collections: collectionRepository{db: db},
		documents:   documentRepository{db: db},
		chunks:      chunkRepository{db: db},
		fts:         ftsRepository{db: db},
		fastFields:  fastFields,
	}
}

type collectionRepository struct{ db *sql.DB }

func (r collectionRepository) getByName(name string) (*Collection, error) {
	c := &Collection{}
	var parserName, backend sql.NullString
	err := r.db.QueryRow(
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

func (r collectionRepository) list() ([]Collection, error) {
	rows, err := r.db.Query(`SELECT id, name, type, path, pattern, parser_name, parser_version, backend, created_at, updated_at FROM collections ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var collections []Collection
	for rows.Next() {
		var collection Collection
		var parserName, backend sql.NullString
		if err := rows.Scan(&collection.ID, &collection.Name, &collection.Type, &collection.Path, &collection.Pattern, &parserName, &collection.ParserVersion, &backend, &collection.CreatedAt, &collection.UpdatedAt); err != nil {
			return nil, err
		}
		collection.ParserName = parserName.String
		collection.Backend = backend.String
		collections = append(collections, collection)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return collections, nil
}

type documentRepository struct{ db *sql.DB }

func (r documentRepository) getContext(ctx context.Context, collectionID int64, path string) (*Document, error) {
	document := &Document{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, collection_id, path, title, content_hash, mtime, line_count, created_at, updated_at
		 FROM documents WHERE collection_id = ? AND path = ?`, collectionID, path,
	).Scan(&document.ID, &document.CollectionID, &document.Path, &document.Title, &document.ContentHash, &document.Mtime, &document.LineCount, &document.CreatedAt, &document.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return document, nil
}

type chunkRepository struct{ db *sql.DB }

func (r chunkRepository) contentContext(ctx context.Context, chunkID int64) (string, error) {
	var content string
	var compressed []byte
	if err := r.db.QueryRowContext(ctx, `SELECT content, content_zstd FROM chunks WHERE id = ?`, chunkID).Scan(&content, &compressed); err != nil {
		return "", err
	}
	if len(compressed) == 0 {
		return content, nil
	}
	decompressed, err := DecompressString(compressed)
	if err != nil {
		return "", err
	}
	return decompressed, nil
}

type ftsRepository struct{ db *sql.DB }

type vectorRepository struct{ index VectorIndex }

func (r *vectorRepository) set(index VectorIndex) { r.index = index }

func (r vectorRepository) current() VectorIndex { return r.index }
