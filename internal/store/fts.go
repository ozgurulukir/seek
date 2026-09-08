package store

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
)

// FTS5 keyword index: schema lifecycle (initFTS/ftsNeedsRebuild/
// rebuildFTSFromDocuments) and CRUD/search over documents_fts. Extracted
// from store.go as part of the god-object decomposition — a mechanical
// move, no changes.

func (s *Store) initFTS() error {
	// FTS5 table: rebuild if the tokenizer config changed (e.g. upgrading from
	// the old "unicode61" to the Turkish-aware "unicode61 remove_diacritics 2").
	// A tokenizer change requires re-indexing all content, so we drop and
	// recreate the virtual table, then repopulate it from chunk contents.
	needRebuild, err := s.ftsNeedsRebuild()
	if err != nil {
		return fmt.Errorf("check fts tokenize: %w", err)
	}
	// Wrap the whole rebuild (DROP + CREATE + repopulate) in a transaction so
	// a crash mid-migration cannot leave documents_fts half-populated — that
	// would silently break BM25 search, and the new tokenize string would
	// already be in sqlite_master so the migration would never re-trigger.
	if needRebuild {
		if _, err := s.db.Exec(`BEGIN`); err != nil {
			return fmt.Errorf("begin fts rebuild tx: %w", err)
		}
		if _, err := s.db.Exec(`DROP TABLE IF EXISTS documents_fts_vocab`); err != nil {
			s.db.Exec(`ROLLBACK`)
			return fmt.Errorf("drop documents_fts_vocab: %w", err)
		}
		if _, err := s.db.Exec(`DROP TABLE IF EXISTS documents_fts`); err != nil {
			s.db.Exec(`ROLLBACK`)
			return fmt.Errorf("drop documents_fts: %w", err)
		}
	}
	// NOTE: FTS5 requires the tokenize argument as a literal in the DDL —
	// it rejects bound parameters ("tokenize=?") with a parse error. FTSTokenize
	// is a package constant we control, so formatting it in is safe.
	ftsDDL := fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
			title, content,
			content_rowid='id',
			tokenize='%s')`,
		FTSTokenize,
	)
	if _, err := s.db.Exec(ftsDDL); err != nil {
		if needRebuild {
			s.db.Exec(`ROLLBACK`)
		}
		return fmt.Errorf("create documents_fts: %w", err)
	}
	// Create vocab table for zero-memory, instant prefix autocompletion
	vocabDDL := `CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts_vocab USING fts5vocab(documents_fts, 'row')`
	if _, err := s.db.Exec(vocabDDL); err != nil {
		// Non-fatal if sqlite environment lacks fts5vocab
		_ = err
	}
	if needRebuild {
		if err := s.rebuildFTSFromDocuments(); err != nil {
			s.db.Exec(`ROLLBACK`)
			return fmt.Errorf("rebuild fts: %w", err)
		}
		if _, err := s.db.Exec(`COMMIT`); err != nil {
			return fmt.Errorf("commit fts rebuild: %w", err)
		}
	}
	return nil
}

// ftsNeedsRebuild reports whether documents_fts is missing or was created
// with a tokenizer different from the current FTSTokenize config.
func (s *Store) ftsNeedsRebuild() (bool, error) {
	var ddlSQL string
	err := s.db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='documents_fts'`,
	).Scan(&ddlSQL)
	if err == sql.ErrNoRows {
		// Table doesn't exist yet — CREATE will handle it, no rebuild needed.
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !strings.Contains(ddlSQL, FTSTokenize), nil
}

// rebuildFTSFromDocuments repopulates documents_fts from the chunks table.
//
// Fidelity note: the original document body is not retained after chunking,
// so we reconstruct each document's searchable text by concatenating its
// chunks in seq order. This is a lossy approximation of the source —
// ChunkMarkdown drops empty sections and adds overlap, ChunkConversation
// rejoins lines — but for BM25 (a bag-of-words model) term coverage is
// essentially preserved. Snippet rendering may differ slightly from a fresh
// index. Ordering is done in Go (not via SQL GROUP_CONCAT, whose row order
// under an inner subquery ORDER BY is not guaranteed by SQLite).
func (s *Store) rebuildFTSFromDocuments() error {
	// One pass: stream (doc_id, title, chunk_seq, chunk_content) ordered so
	// all chunks of a document arrive together and in seq order.
	rows, err := s.db.Query(
		`SELECT d.id, d.title, ch.seq, ch.content, ch.content_zstd
		   FROM documents d
		   LEFT JOIN chunks ch ON ch.document_id = d.id
		  ORDER BY d.id, ch.seq`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var (
		curID    int64
		curTitle string
		started  bool
		b        strings.Builder
	)
	// flush inserts the accumulated content for the previous document.
	flush := func() error {
		if !started {
			return nil
		}
		_, err := s.db.Exec(
			`INSERT INTO documents_fts (rowid, title, content) VALUES (?, ?, ?)`,
			curID, curTitle, b.String(),
		)
		b.Reset()
		return err
	}

	for rows.Next() {
		var (
			id          int64
			title       string
			seq         sql.NullInt64
			content     sql.NullString
			contentZstd []byte
		)
		if err := rows.Scan(&id, &title, &seq, &content, &contentZstd); err != nil {
			return err
		}
		if !started || id != curID {
			if err := flush(); err != nil {
				return err
			}
			curID, curTitle, started = id, title, true
		}
		text := ""
		if len(contentZstd) > 0 {
			decomp, err := DecompressString(contentZstd)
			if err == nil {
				text = decomp
			}
		} else if content.Valid {
			text = content.String
		}
		if text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(text)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}

// execIgnoreDuplicate executes an ALTER TABLE statement and ignores "duplicate column" errors.
// Other errors are logged to stderr so migration failures are not silently lost.

// --- FTS ---

func (s *Store) UpsertFTS(docID int64, title, content string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID); err != nil {
		return fmt.Errorf("delete fts entry: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO documents_fts (rowid, title, content) VALUES (?, ?, ?)`, docID, title, content); err != nil {
		return fmt.Errorf("insert fts entry: %w", err)
	}
	return tx.Commit()
}

// AppendFTS appends content to an existing FTS entry, preserving earlier text and title.
func (s *Store) AppendFTS(docID int64, newContent string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var existingTitle, existingContent string
	err = tx.QueryRow(`SELECT title, content FROM documents_fts WHERE rowid = ?`, docID).Scan(&existingTitle, &existingContent)
	if err != nil {
		// No existing entry — just insert
		if _, err := tx.Exec(`INSERT INTO documents_fts (rowid, title, content) VALUES (?, '', ?)`, docID, newContent); err != nil {
			return err
		}
		return tx.Commit()
	}
	combined := existingContent + "\n" + newContent
	if _, err := tx.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID); err != nil {
		return fmt.Errorf("delete fts entry: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO documents_fts (rowid, title, content) VALUES (?, ?, ?)`, docID, existingTitle, combined); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SearchFTS(query string, limit int, filters *FilterSet) ([]SearchResult, error) {
	// bm25 column weights follow the table's column order (title, content),
	// so title matches (FTSTitleWeight=10.0) rank above body matches (1.0).
	sqlQuery := `SELECT d.id, d.title, d.path, c.name, snippet(documents_fts, 1, '>>>', '<<<', '...', 40) as snip, bm25(documents_fts, ?, 1.0)
		 FROM documents_fts f
		 JOIN documents d ON d.id = f.rowid
		 JOIN collections c ON c.id = d.collection_id
		 WHERE documents_fts MATCH ?`
	var args []interface{}
	args = append(args, FTSTitleWeight, query)
	if filters != nil {
		clause, fargs, err := filters.ToSQL()
		if err != nil {
			return nil, err
		}
		if clause != "" {
			sqlQuery += " AND " + clause
			args = append(args, fargs...)
		}
	}
	sqlQuery += " ORDER BY bm25(documents_fts, ?, 1.0) LIMIT ?"
	args = append(args, FTSTitleWeight, limit)

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []SearchResult
	var docIDs []int64
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.DocumentID, &r.Title, &r.Path, &r.Collection, &r.Content, &r.Score); err != nil {
			return nil, err
		}
		results = append(results, r)
		docIDs = append(docIDs, r.DocumentID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search fts rows: %w", err)
	}

	// Populate line numbers from first chunk for all results in a single batch query
	if len(docIDs) > 0 {
		placeholders := make([]string, len(docIDs))
		docArgs := make([]interface{}, len(docIDs))
		for i, id := range docIDs {
			placeholders[i] = "?"
			docArgs[i] = id
		}
		chunkQuery := fmt.Sprintf(`SELECT document_id, COALESCE(start_line, 0), COALESCE(end_line, 0)
			FROM (
				SELECT document_id, start_line, end_line,
				       ROW_NUMBER() OVER (PARTITION BY document_id ORDER BY seq ASC) as rn
				FROM chunks WHERE document_id IN (%s)
			) WHERE rn = 1`, strings.Join(placeholders, ","))
		chunkRows, err := s.db.Query(chunkQuery, docArgs...)
		if err != nil {
			log.Printf("WARN: line-span query failed for %d documents: %v", len(docIDs), err)
		} else {
			defer chunkRows.Close()
			type lineSpan struct {
				start, end int
			}
			spans := make(map[int64]lineSpan, len(docIDs))
			for chunkRows.Next() {
				var docID int64
				var sLine, eLine int
				if err := chunkRows.Scan(&docID, &sLine, &eLine); err != nil {
					log.Printf("WARN: line-span row scan failed: %v", err)
					continue
				}
				spans[docID] = lineSpan{start: sLine, end: eLine}
			}
			if err := chunkRows.Err(); err != nil {
				log.Printf("WARN: line-span query iteration failed: %v", err)
			}
			for i := range results {
				if span, ok := spans[results[i].DocumentID]; ok {
					results[i].StartLine = span.start
					results[i].EndLine = span.end
				}
			}
		}
	}
	return results, nil
}
