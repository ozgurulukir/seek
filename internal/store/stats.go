package store

import (
	"fmt"
	"strings"
)

// Counters and autocomplete. Extracted from store.go as part of the
// god-object decomposition — a mechanical move, no changes.

// --- Stats ---

func (s *Store) CountDocuments(collectionID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE collection_id = ?`, collectionID).Scan(&n)
	return n, err
}

func (s *Store) CountChunks(collectionID int64) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM chunks WHERE document_id IN (SELECT id FROM documents WHERE collection_id = ?)`,
		collectionID,
	).Scan(&n)
	return n, err
}

func (s *Store) AutocompleteTerms(prefix string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 10
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return nil, nil
	}
	endPrefix := prefix + "\uffff"
	query := `SELECT term FROM documents_fts_vocab WHERE term >= ? AND term < ? ORDER BY term LIMIT ?`
	rows, err := s.db.Query(query, prefix, endPrefix, limit)
	if err != nil {
		return nil, fmt.Errorf("autocomplete query: %w", err)
	}
	defer rows.Close()

	var terms []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err == nil {
			terms = append(terms, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("autocomplete rows: %w", err)
	}
	return terms, nil
}
