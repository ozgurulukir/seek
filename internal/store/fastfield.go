package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// FastFieldStore provides fast field storage for sorting and aggregation.
// It uses a dedicated SQLite table with JSON-encoded values.
type FastFieldStore struct {
	db *sql.DB
}

// NewFastFieldStore creates a new fast field store.
func NewFastFieldStore(db *sql.DB) *FastFieldStore {
	return &FastFieldStore{db: db}
}

// ensureTable creates the fast_fields table if it doesn't exist.
func (f *FastFieldStore) ensureTable() error {
	_, err := f.db.Exec(`CREATE TABLE IF NOT EXISTS fast_fields (
		doc_id INTEGER NOT NULL,
		field_name TEXT NOT NULL,
		field_value TEXT,
		PRIMARY KEY (doc_id, field_name)
	)`)
	if err != nil {
		return err
	}
	// Index for value lookups (FastFieldFilter: WHERE field_name = ? AND field_value = ?).
	// The PK (doc_id, field_name) cannot serve these because it leads with doc_id,
	// so without this index such filters fall back to a full table scan.
	_, err = f.db.Exec(`CREATE INDEX IF NOT EXISTS idx_fast_fields_name_value ON fast_fields (field_name, field_value)`)
	return err
}

// Set stores a fast field value for a document.
func (f *FastFieldStore) Set(docID int64, fieldName string, value interface{}) error {
	if err := f.ensureTable(); err != nil {
		return err
	}

	encoded, err := encodeFastFieldValue(value)
	if err != nil {
		return err
	}

	_, err = f.db.Exec(
		`INSERT OR REPLACE INTO fast_fields (doc_id, field_name, field_value) VALUES (?, ?, ?)`,
		docID, fieldName, encoded,
	)
	return err
}

// Get retrieves a fast field value for a document.
func (f *FastFieldStore) Get(docID int64, fieldName string) (interface{}, error) {
	if err := f.ensureTable(); err != nil {
		return nil, err
	}

	var encoded sql.NullString
	err := f.db.QueryRow(
		`SELECT field_value FROM fast_fields WHERE doc_id = ? AND field_name = ?`,
		docID, fieldName,
	).Scan(&encoded)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if !encoded.Valid || encoded.String == "" {
		return nil, nil
	}

	return decodeFastFieldValue(encoded.String)
}

// BatchGet retrieves fast field values for multiple documents.
func (f *FastFieldStore) BatchGet(docIDs []int64, fieldName string) (map[int64]interface{}, error) {
	if err := f.ensureTable(); err != nil {
		return nil, err
	}

	if len(docIDs) == 0 {
		return make(map[int64]interface{}), nil
	}

	placeholders := make([]string, len(docIDs))
	args := make([]interface{}, len(docIDs))
	for i, id := range docIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT doc_id, field_value FROM fast_fields WHERE doc_id IN (%s) AND field_name = ?`,
		strings.Join(placeholders, ","),
	)
	args = append(args, fieldName)

	rows, err := f.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]interface{})
	for rows.Next() {
		var docID int64
		var encoded string
		if err := rows.Scan(&docID, &encoded); err != nil {
			return nil, err
		}
		val, err := decodeFastFieldValue(encoded)
		if err != nil {
			return nil, err
		}
		result[docID] = val
	}

	return result, nil
}

// Delete removes a fast field value for a document.
func (f *FastFieldStore) Delete(docID int64, fieldName string) error {
	if err := f.ensureTable(); err != nil {
		return err
	}
	_, err := f.db.Exec(
		`DELETE FROM fast_fields WHERE doc_id = ? AND field_name = ?`,
		docID, fieldName,
	)
	return err
}

// DeleteForDocument removes all fast field values for a document.
func (f *FastFieldStore) DeleteForDocument(docID int64) error {
	if err := f.ensureTable(); err != nil {
		return err
	}
	_, err := f.db.Exec(`DELETE FROM fast_fields WHERE doc_id = ?`, docID)
	return err
}

// encodeFastFieldValue encodes a value to a JSON string for storage.
func encodeFastFieldValue(value interface{}) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// decodeFastFieldValue decodes a JSON string to a Go value.
func decodeFastFieldValue(encoded string) (interface{}, error) {
	var value interface{}
	if err := json.Unmarshal([]byte(encoded), &value); err != nil {
		return nil, err
	}
	return value, nil
}
