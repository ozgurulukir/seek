package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// IndexChunk is the persistence representation of one searchable chunk.
// Keeping this type in store makes the document/FTS/chunk write boundary
// explicit without exposing *sql.Tx to indexers.
type IndexChunk struct {
	Seq       int
	Content   string
	StartLine int
	EndLine   int
	ChunkType ChunkType
	ImagePath string
}

// DocumentIndex describes a complete document index update. Replace writes
// atomically update the document row, FTS row, chunks, and fast fields.
type DocumentIndex struct {
	CollectionID int64
	Path         string
	Title        string
	ContentHash  string
	Mtime        float64
	LineCount    int
	FTSContent   string
	Chunks       []IndexChunk
	FastFields   map[string]string
}

// UpsertAndReplaceIndex atomically replaces all searchable state for a
// document. If any part fails, the previous document, FTS row, chunks, and
// fast fields remain visible.
func (s *Store) UpsertAndReplaceIndex(ctx context.Context, req DocumentIndex) (int64, error) {
	return s.writeIndex(ctx, req, true)
}

// UpsertAndAppendIndex atomically appends FTS content and chunks to a
// document. Existing chunks and fast fields are retained.
func (s *Store) UpsertAndAppendIndex(ctx context.Context, req DocumentIndex) (int64, error) {
	return s.writeIndex(ctx, req, false)
}

func (s *Store) writeIndex(ctx context.Context, req DocumentIndex, replace bool) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin index transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	var docID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO documents (collection_id, path, title, content_hash, mtime, line_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(collection_id, path) DO UPDATE SET
		  title = excluded.title,
		  content_hash = excluded.content_hash,
		  mtime = excluded.mtime,
		  line_count = excluded.line_count,
		  updated_at = excluded.updated_at
		RETURNING id`,
		req.CollectionID, req.Path, req.Title, req.ContentHash, req.Mtime, req.LineCount, now, now,
	).Scan(&docID)
	if err != nil {
		return 0, fmt.Errorf("upsert document: %w", err)
	}

	if replace {
		if _, err := tx.ExecContext(ctx, `DELETE FROM documents_fts WHERE rowid = ?`, docID); err != nil {
			return 0, fmt.Errorf("delete fts entry: %w", err)
		}
	}
	if !replace {
		var title, content string
		err := tx.QueryRowContext(ctx, `SELECT title, content FROM documents_fts WHERE rowid = ?`, docID).Scan(&title, &content)
		switch err {
		case nil:
			if content != "" && req.FTSContent != "" {
				content += "\n"
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM documents_fts WHERE rowid = ?`, docID); err != nil {
				return 0, fmt.Errorf("replace appended fts entry: %w", err)
			}
			req.FTSContent = content + req.FTSContent
			if req.Title == "" {
				req.Title = title
			}
		case sql.ErrNoRows:
		default:
			return 0, fmt.Errorf("read existing fts entry: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO documents_fts (rowid, title, content) VALUES (?, ?, ?)`, docID, req.Title, req.FTSContent); err != nil {
		return 0, fmt.Errorf("insert fts entry: %w", err)
	}

	if replace {
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, docID); err != nil {
			return 0, fmt.Errorf("delete chunks: %w", err)
		}
		if err := ensureFastFieldsTx(ctx, tx); err != nil {
			return 0, fmt.Errorf("ensure fast fields: %w", err)
		}
		// Only clear fast fields when the caller supplied a computed set
		// (e.g. semantic enrichment). A nil FastFields map means the caller
		// did not (re)compute metadata this pass (semantic disabled or the
		// service is down), so keep the previously stored values rather than
		// wiping them — enrichment is optional and must not destroy existing
		// metadata on a transient outage.
		if req.FastFields != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM fast_fields WHERE doc_id = ?`, docID); err != nil {
				return 0, fmt.Errorf("delete fast fields: %w", err)
			}
		}
	} else if len(req.FastFields) > 0 {
		if err := ensureFastFieldsTx(ctx, tx); err != nil {
			return 0, fmt.Errorf("ensure fast fields: %w", err)
		}
	}

	for _, ch := range req.Chunks {
		if err := insertIndexChunk(ctx, tx, docID, ch, s.compressionEnabled, s.compressionLevel, now); err != nil {
			return 0, err
		}
	}
	for field, value := range req.FastFields {
		if value == "" {
			continue
		}
		encoded, err := encodeFastFieldValue(value)
		if err != nil {
			return 0, fmt.Errorf("encode fast field %q: %w", field, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO fast_fields (doc_id, field_name, field_value) VALUES (?, ?, ?)`, docID, field, encoded); err != nil {
			return 0, fmt.Errorf("write fast field %q: %w", field, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit index transaction: %w", err)
	}
	return docID, nil
}

func ensureFastFieldsTx(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS fast_fields (
		doc_id INTEGER NOT NULL,
		field_name TEXT NOT NULL,
		field_value TEXT,
		PRIMARY KEY (doc_id, field_name)
	)`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_fast_fields_name_value ON fast_fields (field_name, field_value)`)
	return err
}

func insertIndexChunk(ctx context.Context, tx *sql.Tx, docID int64, ch IndexChunk, compressionEnabled bool, compressionLevel int, now string) error {
	if ch.ChunkType != ChunkTypeText && ch.ChunkType != ChunkTypeImage {
		return fmt.Errorf("insert chunk %d: unsupported chunk type %d", ch.Seq, ch.ChunkType)
	}
	var contentZstd []byte
	var err error
	if compressionEnabled {
		contentZstd, err = CompressString(ch.Content, compressionLevel)
		if err != nil {
			return fmt.Errorf("compress chunk %d: %w", ch.Seq, err)
		}
	}
	if ch.ChunkType == ChunkTypeImage {
		_, err = tx.ExecContext(ctx, `INSERT INTO chunks (document_id, seq, content, content_zstd, embedding, chunk_type, image_path, start_line, end_line, created_at) VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, ?)`, docID, ch.Seq, ch.Content, contentZstd, ch.ChunkType, ch.ImagePath, ch.StartLine, ch.EndLine, now)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO chunks (document_id, seq, content, content_zstd, embedding, chunk_type, image_path, start_line, end_line, created_at) VALUES (?, ?, ?, ?, NULL, ?, NULL, ?, ?, ?)`, docID, ch.Seq, ch.Content, contentZstd, ch.ChunkType, ch.StartLine, ch.EndLine, now)
	}
	if err != nil {
		return fmt.Errorf("insert chunk %d: %w", ch.Seq, err)
	}
	return nil
}
