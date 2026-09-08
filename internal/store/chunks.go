package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Chunk CRUD and content access. Extracted from store.go as part of the
// god-object decomposition — a mechanical move, no changes.

// --- Chunks ---

func (s *Store) DeleteChunksForDocument(docID int64) error {
	_, err := s.db.Exec(`DELETE FROM chunks WHERE document_id = ?`, docID)
	return err
}

// MaxChunkSeq returns the highest seq among a document's chunks, or 0 when the
// document has no chunks. Callers inserting new chunks (e.g. incremental
// conversation sync) use MaxChunkSeq+1 to continue the sequence instead of
// restarting at 0 and colliding with earlier chunks.
func (s *Store) MaxChunkSeq(docID int64) (int, error) {
	var seq sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(seq) FROM chunks WHERE document_id = ?`, docID).Scan(&seq); err != nil {
		return 0, err
	}
	if !seq.Valid {
		return 0, nil
	}
	return int(seq.Int64), nil
}

func (s *Store) InsertChunk(docID int64, seq int, content string, embedding []float32) error {
	return s.InsertChunkWithLines(docID, seq, content, 0, 0, embedding)
}

// InsertChunkWithLines inserts a chunk with line range metadata.
func (s *Store) InsertChunkWithLines(docID int64, seq int, content string, startLine, endLine int, embedding []float32) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var contentZstd []byte
	var err error
	if s.compressionEnabled {
		contentZstd, err = CompressString(content, s.compressionLevel)
		if err != nil {
			return fmt.Errorf("compress chunk: %w", err)
		}
	}
	_, err = s.db.Exec(
		`INSERT INTO chunks (document_id, seq, content, content_zstd, embedding, chunk_type, image_path, start_line, end_line, created_at) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)`,
		docID, seq, content, contentZstd, encodeEmbedding(embedding), ChunkTypeText, startLine, endLine, now,
	)
	return err
}

// GetSurroundingContext fetches adjacent chunks within radius for a document and returns the combined content with expanded line span.
func (s *Store) GetSurroundingContext(docID int64, seq int, radius int) (string, int, int, error) {
	if radius <= 0 {
		radius = 0
	}
	minSeq := seq - radius
	if minSeq < 0 {
		minSeq = 0
	}
	maxSeq := seq + radius

	rows, err := s.db.Query(
		`SELECT seq, content, content_zstd, COALESCE(start_line, 0), COALESCE(end_line, 0)
		 FROM chunks
		 WHERE document_id = ? AND seq >= ? AND seq <= ?
		 ORDER BY seq ASC`,
		docID, minSeq, maxSeq,
	)
	if err != nil {
		return "", 0, 0, err
	}
	defer rows.Close()

	var contents []string
	startLine := 0
	endLine := 0

	for rows.Next() {
		var (
			sNum         int
			content      string
			contentZstd  []byte
			sLine, eLine int
		)
		if err := rows.Scan(&sNum, &content, &contentZstd, &sLine, &eLine); err != nil {
			continue
		}
		if len(contentZstd) > 0 {
			if decomp, err := DecompressString(contentZstd); err == nil {
				content = decomp
			}
		}
		if content != "" {
			contents = append(contents, content)
		}
		if startLine == 0 || (sLine > 0 && sLine < startLine) {
			startLine = sLine
		}
		if eLine > endLine {
			endLine = eLine
		}
	}
	if err := rows.Err(); err != nil {
		return "", 0, 0, fmt.Errorf("get surrounding context rows: %w", err)
	}
	return strings.Join(contents, "\n\n"), startLine, endLine, nil
}

// InsertImageChunk inserts an image chunk with type=1 and an image path.
func (s *Store) InsertImageChunk(docID int64, seq int, context string, imagePath string, embedding []float32) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var contentZstd []byte
	var err error
	if s.compressionEnabled {
		contentZstd, err = CompressString(context, s.compressionLevel)
		if err != nil {
			return fmt.Errorf("compress image chunk: %w", err)
		}
	}
	_, err = s.db.Exec(
		`INSERT INTO chunks (document_id, seq, content, content_zstd, embedding, chunk_type, image_path, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		docID, seq, context, contentZstd, encodeEmbedding(embedding), ChunkTypeImage, imagePath, now,
	)
	return err
}

// GetChunkContent returns the content of a chunk, decompressing if necessary.
func (s *Store) GetChunkContent(chunkID int64) (string, error) {
	var content string
	var contentZstd []byte
	err := s.db.QueryRow(
		`SELECT content, content_zstd FROM chunks WHERE id = ?`,
		chunkID,
	).Scan(&content, &contentZstd)
	if err != nil {
		return "", err
	}
	if len(contentZstd) > 0 {
		decompressed, err := DecompressString(contentZstd)
		if err != nil {
			return "", fmt.Errorf("decompress chunk %d: %w", chunkID, err)
		}
		return decompressed, nil
	}
	return content, nil
}

func (s *Store) UpdateChunkEmbedding(chunkID int64, embedding []float32) error {
	_, err := s.db.Exec(`UPDATE chunks SET embedding = ? WHERE id = ?`, encodeEmbedding(embedding), chunkID)
	if err != nil {
		return err
	}
	if s.vectorIndex != nil {
		if err := s.vectorIndex.Add(chunkID, embedding); err != nil {
			return fmt.Errorf("update vector index: %w", err)
		}
	}
	return nil
}

// GetChunksWithoutEmbedding returns chunks that don't have embeddings yet.
// If force is true, returns all chunks.
func (s *Store) GetChunksWithoutEmbedding(force bool) ([]Chunk, error) {
	return s.getChunksWithoutEmbedding(force, "", false)
}

// GetChunksWithoutEmbeddingForCollectionType returns chunks from collections
// of typ whose embeddings are missing (or all chunks when force is true).
func (s *Store) GetChunksWithoutEmbeddingForCollectionType(typ CollectionType, force bool) ([]Chunk, error) {
	return s.getChunksWithoutEmbedding(force, typ, true)
}

func (s *Store) getChunksWithoutEmbedding(force bool, typ CollectionType, filterType bool) ([]Chunk, error) {
	query := `SELECT chunks.id, chunks.document_id, chunks.seq, chunks.content, chunks.content_zstd, COALESCE(chunks.chunk_type, ?), COALESCE(chunks.image_path, '') FROM chunks`
	args := []interface{}{ChunkTypeText}
	if filterType {
		query += " JOIN documents d ON d.id = chunks.document_id JOIN collections c ON c.id = d.collection_id"
	}
	if !force {
		query += " WHERE chunks.embedding IS NULL"
	}
	if filterType {
		if force {
			query += " WHERE"
		} else {
			query += " AND"
		}
		query += " c.type = ?"
		args = append(args, typ)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chunks []Chunk
	for rows.Next() {
		var ch Chunk
		var contentZstd []byte
		if err := rows.Scan(&ch.ID, &ch.DocumentID, &ch.Seq, &ch.Content, &contentZstd, &ch.ChunkType, &ch.ImagePath); err != nil {
			return nil, err
		}
		if len(contentZstd) > 0 {
			ch.Content, err = DecompressString(contentZstd)
			if err != nil {
				return nil, fmt.Errorf("decompress chunk %d: %w", ch.ID, err)
			}
		}
		chunks = append(chunks, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get chunks without embedding rows: %w", err)
	}
	return chunks, nil
}
