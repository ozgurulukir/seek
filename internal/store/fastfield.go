package store

import (
	"context"
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

// Set stores a fast field value for a document.
func (f *FastFieldStore) Set(docID int64, fieldName string, value interface{}) error {
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
	return f.BatchGetContext(context.Background(), docIDs, fieldName)
}

// BatchGetContext retrieves fast fields while honoring query cancellation.
func (f *FastFieldStore) BatchGetContext(ctx context.Context, docIDs []int64, fieldName string) (map[int64]interface{}, error) {
	if len(docIDs) == 0 {
		return make(map[int64]interface{}), nil
	}

	result := make(map[int64]interface{}, len(docIDs))
	const batchSize = 500

	for i := 0; i < len(docIDs); i += batchSize {
		end := i + batchSize
		if end > len(docIDs) {
			end = len(docIDs)
		}
		chunk := docIDs[i:end]

		args := make([]any, len(chunk)+1)
		for j, id := range chunk {
			args[j] = id
		}
		args[len(chunk)] = fieldName

		query := `SELECT doc_id, field_value FROM fast_fields WHERE doc_id IN (` + sqlPlaceholders(len(chunk)) + `) AND field_name = ?`
		rows, err := f.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var docID int64
			var encoded string
			if err := rows.Scan(&docID, &encoded); err != nil {
				rows.Close()
				return nil, err
			}
			val, err := decodeFastFieldValue(encoded)
			if err != nil {
				rows.Close()
				return nil, err
			}
			result[docID] = val
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// Delete removes a fast field value for a document.
func (f *FastFieldStore) Delete(docID int64, fieldName string) error {
	_, err := f.db.Exec(
		`DELETE FROM fast_fields WHERE doc_id = ? AND field_name = ?`,
		docID, fieldName,
	)
	return err
}

// DeleteForDocument removes all fast field values for a document.
func (f *FastFieldStore) DeleteForDocument(docID int64) error {
	_, err := f.db.Exec(`DELETE FROM fast_fields WHERE doc_id = ?`, docID)
	return err
}

// ListForDocument returns every fast field of one document decoded to plain
// text. The semantic backfill reads the current fields so the merged
// native+semantic set it writes via UpdateSemanticState preserves them.
func (f *FastFieldStore) ListForDocument(documentID int64) (map[string]string, error) {
	return f.ListForDocumentContext(context.Background(), documentID)
}

// ListForDocumentContext is the cancellation-aware variant of ListForDocument.
func (f *FastFieldStore) ListForDocumentContext(ctx context.Context, documentID int64) (map[string]string, error) {
	rows, err := f.db.QueryContext(ctx,
		`SELECT field_name, field_value FROM fast_fields WHERE doc_id = ?`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields := make(map[string]string)
	for rows.Next() {
		var name, encoded string
		if err := rows.Scan(&name, &encoded); err != nil {
			return nil, err
		}
		if value := decodeFastFieldText(encoded); value != "" {
			fields[name] = value
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return fields, nil
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

// decodeFastFieldText decodes a stored fast-field value to plain text.
// String values (the only kind production writers store) decode faithfully —
// including values that contain quotes, which the legacy SQL
// REPLACE(field_value, '"', ”) idiom corrupted. Non-string JSON and
// undecodable bytes fall back to best-effort text. Quote-delimited,
// backslash-free input is the exception: its interior bytes are returned
// verbatim even when not valid JSON (e.g. legacy rows with stray interior
// quotes), instead of falling to the best-effort path.
// PERF: the fast path slices quote-delimited backslash-free strings directly,
// skipping the json.Unmarshal allocations; every other input decodes as before.
func decodeFastFieldText(encoded string) string {
	if len(encoded) >= 2 && encoded[0] == '"' && encoded[len(encoded)-1] == '"' && !strings.Contains(encoded, `\`) {
		return encoded[1 : len(encoded)-1]
	}
	var value interface{}
	if err := json.Unmarshal([]byte(encoded), &value); err != nil {
		return strings.ReplaceAll(encoded, `"`, "")
	}
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}
