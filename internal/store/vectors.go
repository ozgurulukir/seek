package store

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"github.com/viterin/vek/vek32"
	"math"
	"sort"
	"strings"
)

// Vector semantic search and the HNSW index sync lifecycle. Extracted from
// store.go as part of the god-object decomposition — a mechanical move,
// no changes.

func (s *Store) SyncVectorIndex() error {
	_, err := s.syncVectorIndexFull()
	return err
}

func (s *Store) syncVectorIndexFull() (int, error) {
	if s.vectorIndex == nil {
		return 0, nil
	}

	// Clear existing entries so we rebuild from the current DB state.
	if err := s.vectorIndex.Clear(); err != nil {
		return 0, fmt.Errorf("clear vector index: %w", err)
	}

	rows, err := s.db.Query(`SELECT id, embedding FROM chunks WHERE embedding IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	added := 0
	for rows.Next() {
		var chunkID int64
		var embBlob []byte
		if err := rows.Scan(&chunkID, &embBlob); err != nil {
			return added, err
		}
		emb := decodeEmbedding(embBlob)
		if err := s.vectorIndex.Add(chunkID, emb); err != nil {
			return added, err
		}
		added++
	}
	if err := rows.Err(); err != nil {
		return added, fmt.Errorf("sync vector index rows: %w", err)
	}
	return added, nil
}

// SyncVectorIndexIncremental adds newly embedded chunks that are not yet in the vector index.
// If the vector index is empty or contains stale entries from deleted documents, it runs a full sync.
// It scans only chunk IDs first to avoid loading megabytes of embedding BLOBs into memory.
func (s *Store) SyncVectorIndexIncremental() (int, error) {
	if s.vectorIndex == nil {
		return 0, nil
	}

	if s.vectorIndex.Len() == 0 {
		return s.syncVectorIndexFull()
	}

	var dbCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chunks WHERE embedding IS NOT NULL`).Scan(&dbCount); err != nil {
		return 0, err
	}

	// If index has more entries than DB has embedded chunks, chunks were deleted;
	// perform full sync to purge stale entries from the HNSW graph.
	if s.vectorIndex.Len() > dbCount {
		return s.syncVectorIndexFull()
	}

	// Fast ID-only scan: minimal RAM & I/O (does not read multi-megabyte BLOBs)
	rows, err := s.db.Query(`SELECT id FROM chunks WHERE embedding IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var missingIDs []int64
	for rows.Next() {
		var chunkID int64
		if err := rows.Scan(&chunkID); err != nil {
			return 0, err
		}
		if !s.vectorIndex.Contains(chunkID) {
			missingIDs = append(missingIDs, chunkID)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("sync vector index id scan: %w", err)
	}

	if len(missingIDs) == 0 {
		return 0, nil
	}

	// Load and insert embeddings only for missing chunks
	added := 0
	for _, chunkID := range missingIDs {
		var embBlob []byte
		err := s.db.QueryRow(`SELECT embedding FROM chunks WHERE id = ?`, chunkID).Scan(&embBlob)
		if err != nil || len(embBlob) == 0 {
			continue
		}
		emb := decodeEmbedding(embBlob)
		if err := s.vectorIndex.Add(chunkID, emb); err != nil {
			return added, err
		}
		added++
	}

	return added, nil
}

func (s *Store) SearchVector(queryEmb []float32, limit int, filters *FilterSet) ([]SearchResult, error) {
	// Use HNSW index if available
	if s.vectorIndex != nil {
		// HNSW returns chunk IDs; we push filters into the SQL fetch query
		// so the DB handles filtering efficiently. Over-fetch to account for
		// results that get filtered out.
		searchLimit := limit
		if filters != nil {
			searchLimit = limit * 10
		}
		results, err := s.vectorIndex.Search(queryEmb, searchLimit)
		if err == nil && len(results) > 0 {
			fullResults, err := s.fetchSearchResults(results, filters)
			if err != nil {
				return nil, err
			}
			if len(fullResults) > limit {
				fullResults = fullResults[:limit]
			}
			return fullResults, nil
		}
		// Fall through to linear scan on error or empty results
	}

	return s.linearSearchVector(queryEmb, limit, filters)
}

// chunkRow is the shared chunk projection used by the vector-search paths
// (HNSW fetch and linear scan). The embedding blob is scanned separately so
// the same row shape serves both paths.
type chunkRow struct {
	ChunkID    int64
	DocumentID int64
	Seq        int
	Content    string
	ChunkType  ChunkType
	ImagePath  string
	StartLine  int
	EndLine    int
	Title      string
	Path       string
	Collection string
}

// chunkRowColumns is the SELECT projection matching scanChunkRows. The leading
// COALESCE placeholder must be bound to ChunkTypeText as the first query arg.
const chunkRowColumns = `ch.id, ch.document_id, ch.seq, ch.content, ch.content_zstd,
		COALESCE(ch.chunk_type, ?), COALESCE(ch.image_path, ''),
		COALESCE(ch.start_line, 0), COALESCE(ch.end_line, 0),
		d.title, d.path, c.name`

// appendFilterClause pushes filter predicates into a query whose WHERE clause
// already has at least one condition (joined with AND). Filter errors
// propagate so invalid predicates can never silently widen the query.
func appendFilterClause(query string, filters *FilterSet, args []interface{}) (string, []interface{}, error) {
	if filters == nil {
		return query, args, nil
	}
	clause, fargs, err := filters.ToSQL()
	if err != nil {
		return "", nil, err
	}
	if clause == "" {
		return query, args, nil
	}
	return query + " AND " + clause, append(args, fargs...), nil
}

// scanChunkRows drains rows into chunk rows, transparently decompressing
// zstd content. When wantEmbedding is true, rows must carry a 13th column
// with the raw embedding blob, returned positionally in embBlobs.
func scanChunkRows(rows *sql.Rows, wantEmbedding bool) ([]chunkRow, [][]byte, error) {
	var out []chunkRow
	var embBlobs [][]byte
	for rows.Next() {
		var (
			r           chunkRow
			contentZstd []byte
			embBlob     []byte
		)
		var err error
		if wantEmbedding {
			err = rows.Scan(&r.ChunkID, &r.DocumentID, &r.Seq, &r.Content, &contentZstd, &r.ChunkType, &r.ImagePath, &r.StartLine, &r.EndLine, &r.Title, &r.Path, &r.Collection, &embBlob)
		} else {
			err = rows.Scan(&r.ChunkID, &r.DocumentID, &r.Seq, &r.Content, &contentZstd, &r.ChunkType, &r.ImagePath, &r.StartLine, &r.EndLine, &r.Title, &r.Path, &r.Collection)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("scan chunk row: %w", err)
		}
		if len(contentZstd) > 0 {
			r.Content, err = DecompressString(contentZstd)
			if err != nil {
				return nil, nil, fmt.Errorf("decompress chunk %d: %w", r.ChunkID, err)
			}
		}
		out = append(out, r)
		if wantEmbedding {
			embBlobs = append(embBlobs, embBlob)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate chunk rows: %w", err)
	}
	return out, embBlobs, nil
}

// linearSearchVector performs a linear scan over all embedded chunks with optional filters.
func (s *Store) linearSearchVector(queryEmb []float32, limit int, filters *FilterSet) ([]SearchResult, error) {
	sqlQuery := `SELECT ` + chunkRowColumns + `, ch.embedding
		 FROM chunks ch
		 JOIN documents d ON d.id = ch.document_id
		 JOIN collections c ON c.id = d.collection_id
		 WHERE ch.embedding IS NOT NULL`
	args := []interface{}{ChunkTypeText}
	sqlQuery, args, err := appendFilterClause(sqlQuery, filters, args)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chunkRows, embBlobs, err := scanChunkRows(rows, true)
	if err != nil {
		return nil, err
	}

	type scored struct {
		SearchResult
		score float64
	}
	all := make([]scored, 0, len(chunkRows))
	for i, r := range chunkRows {
		emb := decodeEmbedding(embBlobs[i])
		all = append(all, scored{
			SearchResult: SearchResult{
				ChunkID:    r.ChunkID,
				DocumentID: r.DocumentID,
				Seq:        r.Seq,
				Title:      r.Title,
				Path:       r.Path,
				Collection: r.Collection,
				Content:    r.Content,
				ChunkType:  r.ChunkType,
				ImagePath:  r.ImagePath,
				StartLine:  r.StartLine,
				EndLine:    r.EndLine,
			},
			score: cosineSimilarity(queryEmb, emb),
		})
	}

	// Sort by similarity descending
	sort.Slice(all, func(i, j int) bool {
		return all[i].score > all[j].score
	})

	if len(all) > limit {
		all = all[:limit]
	}

	results := make([]SearchResult, len(all))
	for i, s := range all {
		s.SearchResult.Score = s.score
		results[i] = s.SearchResult
	}
	return results, nil
}

// fetchSearchResults fetches full SearchResult data for HNSW results.
// Filters are pushed into the SQL WHERE clause so the DB handles filtering.
func (s *Store) fetchSearchResults(results []VectorResult, filters *FilterSet) ([]SearchResult, error) {
	if len(results) == 0 {
		return nil, nil
	}

	// Fetch all in one query
	ids := make([]int64, len(results))
	for i, r := range results {
		ids[i] = r.ChunkID
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT `+chunkRowColumns+`
		 FROM chunks ch
		 JOIN documents d ON d.id = ch.document_id
		 JOIN collections c ON c.id = d.collection_id
		 WHERE ch.id IN (%s)`,
		strings.Join(placeholders, ","),
	)
	args = append([]interface{}{ChunkTypeText}, args...)

	// Push filters into the SQL query so the DB handles filtering
	query, args, err := appendFilterClause(query, filters, args)
	if err != nil {
		return nil, fmt.Errorf("fetch search results filters: %w", err)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("fetch search results query: %w", err)
	}
	defer rows.Close()

	chunkRows, _, err := scanChunkRows(rows, false)
	if err != nil {
		return nil, fmt.Errorf("fetch search results: %w", err)
	}

	fetched := make(map[int64]chunkRow, len(chunkRows))
	for _, cr := range chunkRows {
		fetched[cr.ChunkID] = cr
	}

	// Preserve HNSW ordering while excluding chunks that failed filters or were deleted
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		cr, ok := fetched[r.ChunkID]
		if !ok {
			continue
		}
		out = append(out, SearchResult{
			ChunkID:    r.ChunkID,
			Score:      r.Score,
			DocumentID: cr.DocumentID,
			Seq:        cr.Seq,
			Title:      cr.Title,
			Path:       cr.Path,
			Collection: cr.Collection,
			Content:    cr.Content,
			ChunkType:  cr.ChunkType,
			ImagePath:  cr.ImagePath,
			StartLine:  cr.StartLine,
			EndLine:    cr.EndLine,
		})
	}
	return out, nil
}

// --- Helpers ---

func encodeEmbedding(v []float32) []byte {
	if v == nil {
		return nil
	}
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeEmbedding(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	sim := vek32.CosineSimilarity(a, b)
	// NaN on zero-magnitude inputs; guard to keep prior behavior.
	if math.IsNaN(float64(sim)) {
		return 0
	}
	return float64(sim)
}

// AutocompleteTerms returns matching terms with the given prefix from the FTS vocab table.
