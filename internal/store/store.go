package store

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/viterin/vek/vek32"
)

type CollectionType string

const (
	CollectionTypeMarkdown CollectionType = "markdown"
	CollectionTypeClaude   CollectionType = "claude"
	CollectionTypeCodex    CollectionType = "codex"
	CollectionTypeImages   CollectionType = "images"
	CollectionTypePDF      CollectionType = "pdf"
	CollectionTypeParser   CollectionType = "parser"
)

type ChunkType int

const (
	ChunkTypeText  ChunkType = 0
	ChunkTypeImage ChunkType = 1
)

type Store struct {
	db                 *sql.DB
	vectorIndex        VectorIndex
	fastFields         *FastFieldStore
	compressionEnabled bool
	compressionLevel   int
}

// Filter is a search filter that can be converted to a SQL WHERE clause.
type Filter interface {
	ToSQL() (clause string, args []interface{})
}

// FilterSet combines multiple filters with AND.
type FilterSet struct {
	filters []Filter
}

func NewFilterSet() *FilterSet {
	return &FilterSet{filters: make([]Filter, 0)}
}

func (fs *FilterSet) Add(f Filter) {
	fs.filters = append(fs.filters, f)
}

func (fs *FilterSet) ToSQL() (string, []interface{}) {
	var clauses []string
	var args []interface{}
	for _, f := range fs.filters {
		c, a := f.ToSQL()
		if c != "" {
			clauses = append(clauses, c)
			args = append(args, a...)
		}
	}
	return strings.Join(clauses, " AND "), args
}

// --- Filter Types ---

// CollectionFilter filters by collection name.
type CollectionFilter struct {
	Name string
}

func (f *CollectionFilter) ToSQL() (string, []interface{}) {
	return "d.collection_id = (SELECT id FROM collections WHERE name = ?)", []interface{}{f.Name}
}

// DocTypeFilter filters by collection type (markdown, claude, codex, images, pdf).
type DocTypeFilter struct {
	Type string
}

func (f *DocTypeFilter) ToSQL() (string, []interface{}) {
	return "c.type = ?", []interface{}{f.Type}
}

// DateRangeFilter filters documents by created_at range.
type DateRangeFilter struct {
	After  string // RFC3339 or empty
	Before string // RFC3339 or empty
}

func (f *DateRangeFilter) ToSQL() (string, []interface{}) {
	var clauses []string
	var args []interface{}
	if f.After != "" {
		clauses = append(clauses, "d.created_at >= ?")
		args = append(args, f.After)
	}
	if f.Before != "" {
		clauses = append(clauses, "d.created_at <= ?")
		args = append(args, f.Before)
	}
	return strings.Join(clauses, " AND "), args
}

// ChunkTypeFilter filters chunks by chunk_type (0=text, 1=image).
type ChunkTypeFilter struct {
	Type int // 0=text, 1=image
}

func (f *ChunkTypeFilter) ToSQL() (string, []interface{}) {
	return "ch.chunk_type = ?", []interface{}{f.Type}
}

// PathFilter filters documents by path pattern (GLOB).
type PathFilter struct {
	Pattern string
}

func (f *PathFilter) ToSQL() (string, []interface{}) {
	// Sanitize: reject path traversal
	if strings.Contains(f.Pattern, "..") {
		return "", nil
	}
	cleaned := filepath.Clean(f.Pattern)
	return "d.path GLOB ?", []interface{}{cleaned}
}

// FastFieldFilter filters documents by a fast-field value (e.g. workspace).
// Uses the fast_fields table for indexed lookups.
type FastFieldFilter struct {
	Field string // fast field name (e.g. "workspace")
	Value string // fast field value
}

func (f *FastFieldFilter) ToSQL() (string, []interface{}) {
	// Fast field values are JSON-encoded on write (see encodeFastFieldValue),
	// so we must JSON-encode the comparison value too.
	encoded, err := encodeFastFieldValue(f.Value)
	if err != nil {
		return "", nil
	}
	return "d.id IN (SELECT doc_id FROM fast_fields WHERE field_name = ? AND field_value = ?)",
		[]interface{}{f.Field, encoded}
}

type Collection struct {
	ID            int64
	Name          string
	Type          CollectionType
	Path          string
	Pattern       string
	ParserName    string // for "parser" collections: the schema name
	ParserVersion int    // for "parser" collections: the detected schema version
	CreatedAt     string
	UpdatedAt     string
}

type Document struct {
	ID           int64
	CollectionID int64
	Path         string
	Title        string
	ContentHash  string
	Mtime        float64
	LineCount    int
	CreatedAt    string
	UpdatedAt    string
}

type Chunk struct {
	ID         int64
	DocumentID int64
	Seq        int
	Content    string
	Embedding  []float32
	ChunkType  ChunkType
	ImagePath  string // path to image file on disk (for image chunks)
	CreatedAt  string
}

type SearchResult struct {
	DocumentID int64
	ChunkID    int64
	Title      string
	Path       string
	Collection string
	Content    string
	Score      float64
	ChunkType  ChunkType
	ImagePath  string // non-empty for image chunks
}

func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Store{db: db, fastFields: NewFastFieldStore(db)}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// DB returns the underlying *sql.DB for direct queries (aggregations, etc.).
func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	return s.db.Close()
}

// SetVectorIndex sets the vector index backend (HNSW or linear scan).
func (s *Store) SetVectorIndex(idx VectorIndex) {
	s.vectorIndex = idx
}

// FastFields returns the fast field store for sorting and aggregation.
func (s *Store) FastFields() *FastFieldStore {
	return s.fastFields
}

// SetCompression configures chunk content compression.
// enabled: if true, new chunks are compressed with Zstd.
// level: Zstd compression level (1-22); 0 means default (3).
func (s *Store) SetCompression(enabled bool, level int) {
	s.compressionEnabled = enabled
	if level <= 0 {
		level = 3
	}
	s.compressionLevel = level
}

// SyncVectorIndex adds all embedded chunks to the vector index.
// It clears the index first to ensure consistency.
func (s *Store) SyncVectorIndex() error {
	if s.vectorIndex == nil {
		return nil
	}
	// Clear existing index entries
	// (We do this by creating a fresh index; HNSW doesn't support bulk clear)
	_ = s.vectorIndex

	rows, err := s.db.Query(`SELECT id, embedding FROM chunks WHERE embedding IS NOT NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var chunkID int64
		var embBlob []byte
		if err := rows.Scan(&chunkID, &embBlob); err != nil {
			return err
		}
		emb := decodeEmbedding(embBlob)
		if err := s.vectorIndex.Add(chunkID, emb); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrate() error {
	// Verify FTS5 is available (requires build tag: -tags "fts5")
	var fts5ok int
	if err := s.db.QueryRow(`SELECT 1 FROM pragma_compile_options WHERE compile_options = 'ENABLE_FTS5'`).Scan(&fts5ok); err != nil {
		return fmt.Errorf("SQLite FTS5 not enabled. Build with: make build (or: go build -tags \"fts5\")")
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS collections (
			id INTEGER PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			type TEXT NOT NULL,
			path TEXT NOT NULL,
			pattern TEXT DEFAULT '**/*.md',
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS documents (
			id INTEGER PRIMARY KEY,
			collection_id INTEGER REFERENCES collections(id) ON DELETE CASCADE,
			path TEXT NOT NULL,
			title TEXT,
			content_hash TEXT,
			mtime REAL,
			line_count INTEGER,
			created_at TEXT,
			updated_at TEXT,
			UNIQUE(collection_id, path)
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
			title, content,
			content_rowid='id',
			tokenize='unicode61'
		)`,
		`CREATE TABLE IF NOT EXISTS chunks (
			id INTEGER PRIMARY KEY,
			document_id INTEGER REFERENCES documents(id) ON DELETE CASCADE,
			seq INTEGER,
			content TEXT,
			embedding BLOB,
			created_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_document ON chunks(document_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:40], err)
		}
	}

	// Add new columns for multimodal support (backward compat via ALTER TABLE)
	alterStmts := []string{
		`ALTER TABLE chunks ADD COLUMN chunk_type INTEGER DEFAULT 0`,
		`ALTER TABLE chunks ADD COLUMN image_path TEXT`,
		`ALTER TABLE documents ADD COLUMN metadata TEXT DEFAULT '{}'`,
		`ALTER TABLE chunks ADD COLUMN content_zstd BLOB`,
		`ALTER TABLE collections ADD COLUMN parser_name TEXT`,
		`ALTER TABLE collections ADD COLUMN parser_version INTEGER DEFAULT 0`,
	}
	for _, stmt := range alterStmts {
		s.execIgnoreDuplicate(stmt)
	}

	return nil
}

// execIgnoreDuplicate executes an ALTER TABLE statement and ignores "duplicate column" errors.
func (s *Store) execIgnoreDuplicate(stmt string) {
	_, err := s.db.Exec(stmt)
	if err != nil {
		// SQLite returns "duplicate column name" when column already exists
		if strings.Contains(err.Error(), "duplicate column") {
			return
		}
		// Ignore other ALTER TABLE errors on existing columns
	}
}

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
	id, _ := res.LastInsertId()
	return &Collection{ID: id, Name: name, Type: typ, Path: path, Pattern: pattern}, nil
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
	var parserName sql.NullString
	err := s.db.QueryRow(
		`SELECT id, name, type, path, pattern, parser_name, parser_version, created_at, updated_at
		 FROM collections WHERE name = ?`, name,
	).Scan(&c.ID, &c.Name, &c.Type, &c.Path, &c.Pattern, &parserName, &c.ParserVersion, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.ParserName = parserName.String
	return c, nil
}

func (s *Store) ListCollections() ([]Collection, error) {
	rows, err := s.db.Query(`SELECT id, name, type, path, pattern, parser_name, parser_version, created_at, updated_at FROM collections ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []Collection
	for rows.Next() {
		var c Collection
		var parserName sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.Path, &c.Pattern, &parserName, &c.ParserVersion, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.ParserName = parserName.String
		cols = append(cols, c)
	}
	return cols, nil
}

func (s *Store) DeleteCollection(id int64) error {
	// Delete FTS entries for documents in this collection
	_, err := s.db.Exec(`DELETE FROM documents_fts WHERE rowid IN (SELECT id FROM documents WHERE collection_id = ?)`, id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM chunks WHERE document_id IN (SELECT id FROM documents WHERE collection_id = ?)`, id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM documents WHERE collection_id = ?`, id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM collections WHERE id = ?`, id)
	return err
}

// --- Documents ---

func (s *Store) GetDocument(collectionID int64, path string) (*Document, error) {
	d := &Document{}
	err := s.db.QueryRow(
		`SELECT id, collection_id, path, title, content_hash, mtime, line_count FROM documents WHERE collection_id = ? AND path = ?`,
		collectionID, path,
	).Scan(&d.ID, &d.CollectionID, &d.Path, &d.Title, &d.ContentHash, &d.Mtime, &d.LineCount)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) UpsertDocument(collectionID int64, path, title, contentHash string, mtime float64, lineCount int) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO documents (collection_id, path, title, content_hash, mtime, line_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(collection_id, path) DO UPDATE SET
		   title = excluded.title,
		   content_hash = excluded.content_hash,
		   mtime = excluded.mtime,
		   line_count = excluded.line_count,
		   updated_at = excluded.updated_at`,
		collectionID, path, title, contentHash, mtime, lineCount, now, now,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	// If it was an update, we need to get the actual ID
	if id == 0 {
		err = s.db.QueryRow(`SELECT id FROM documents WHERE collection_id = ? AND path = ?`, collectionID, path).Scan(&id)
		if err != nil {
			return 0, err
		}
	}
	return id, nil
}

// ListDocumentPaths returns all document paths for a collection.
func (s *Store) ListDocumentPaths(collectionID int64) (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT id, path FROM documents WHERE collection_id = ?`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]int64)
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		m[path] = id
	}
	return m, nil
}

// DeleteDocument removes a document and its chunks/FTS entries.
func (s *Store) DeleteDocument(docID int64) error {
	s.db.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID)
	s.db.Exec(`DELETE FROM chunks WHERE document_id = ?`, docID)
	_, err := s.db.Exec(`DELETE FROM documents WHERE id = ?`, docID)
	return err
}

func (s *Store) UpdateDocumentMtime(docID int64, mtime float64) error {
	_, err := s.db.Exec(`UPDATE documents SET mtime = ? WHERE id = ?`, mtime, docID)
	return err
}

// --- FTS ---

func (s *Store) UpsertFTS(docID int64, title, content string) error {
	// Delete existing then insert (FTS5 doesn't support upsert)
	s.db.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID)
	_, err := s.db.Exec(`INSERT INTO documents_fts (rowid, title, content) VALUES (?, ?, ?)`, docID, title, content)
	return err
}

// AppendFTS appends content to an existing FTS entry, preserving earlier text and title.
func (s *Store) AppendFTS(docID int64, newContent string) error {
	var existingTitle, existingContent string
	err := s.db.QueryRow(`SELECT title, content FROM documents_fts WHERE rowid = ?`, docID).Scan(&existingTitle, &existingContent)
	if err != nil {
		// No existing entry — just insert
		_, err = s.db.Exec(`INSERT INTO documents_fts (rowid, title, content) VALUES (?, '', ?)`, docID, newContent)
		return err
	}
	combined := existingContent + "\n" + newContent
	s.db.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID)
	_, err = s.db.Exec(`INSERT INTO documents_fts (rowid, title, content) VALUES (?, ?, ?)`, docID, existingTitle, combined)
	return err
}

func (s *Store) SearchFTS(query string, limit int, filters *FilterSet) ([]SearchResult, error) {
	sqlQuery := `SELECT d.id, d.title, d.path, c.name, snippet(documents_fts, 1, '>>>', '<<<', '...', 40) as snip, bm25(documents_fts)
		 FROM documents_fts f
		 JOIN documents d ON d.id = f.rowid
		 JOIN collections c ON c.id = d.collection_id`
	var args []interface{}
	if filters != nil {
		clause, fargs := filters.ToSQL()
		if clause != "" {
			sqlQuery += " WHERE " + clause
			args = append(args, fargs...)
		}
	}
	sqlQuery += " ORDER BY bm25(documents_fts) LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.DocumentID, &r.Title, &r.Path, &r.Collection, &r.Content, &r.Score); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// --- Chunks ---

func (s *Store) DeleteChunksForDocument(docID int64) error {
	_, err := s.db.Exec(`DELETE FROM chunks WHERE document_id = ?`, docID)
	return err
}

func (s *Store) InsertChunk(docID int64, seq int, content string, embedding []float32) error {
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
		`INSERT INTO chunks (document_id, seq, content, content_zstd, embedding, chunk_type, image_path, created_at) VALUES (?, ?, ?, ?, ?, ?, NULL, ?)`,
		docID, seq, content, contentZstd, encodeEmbedding(embedding), ChunkTypeText, now,
	)
	return err
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
			fullResults := s.fetchSearchResults(results, filters)
			if len(fullResults) > limit {
				fullResults = fullResults[:limit]
			}
			return fullResults, nil
		}
		// Fall through to linear scan on error or empty results
	}

	// Fallback: linear scan over all embedded chunks with optional filters
	sqlQuery := `SELECT ch.id, ch.document_id, ch.content, ch.content_zstd, ch.embedding,
		        COALESCE(ch.chunk_type, ?), COALESCE(ch.image_path, ''),
		        d.title, d.path, c.name
		 FROM chunks ch
		 JOIN documents d ON d.id = ch.document_id
		 JOIN collections c ON c.id = d.collection_id
		 WHERE ch.embedding IS NOT NULL`
	var args []interface{}
	args = append(args, ChunkTypeText)

	if filters != nil {
		clause, fargs := filters.ToSQL()
		if clause != "" {
			sqlQuery += " AND " + clause
			args = append(args, fargs...)
		}
	}

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		SearchResult
		score float64
	}
	var all []scored

	for rows.Next() {
		var (
			chunkID     int64
			docID       int64
			content     string
			contentZstd []byte
			embBlob     []byte
			chunkType   ChunkType
			imagePath   string
			title       string
			path        string
			collection  string
		)
		if err := rows.Scan(&chunkID, &docID, &content, &contentZstd, &embBlob, &chunkType, &imagePath, &title, &path, &collection); err != nil {
			return nil, err
		}
		if len(contentZstd) > 0 {
			content, err = DecompressString(contentZstd)
			if err != nil {
				return nil, fmt.Errorf("decompress chunk %d: %w", chunkID, err)
			}
		}
		emb := decodeEmbedding(embBlob)
		sim := cosineSimilarity(queryEmb, emb)
		all = append(all, scored{
			SearchResult: SearchResult{
				ChunkID:    chunkID,
				DocumentID: docID,
				Title:      title,
				Path:       path,
				Collection: collection,
				Content:    content,
				ChunkType:  chunkType,
				ImagePath:  imagePath,
			},
			score: sim,
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
func (s *Store) fetchSearchResults(results []VectorResult, filters *FilterSet) []SearchResult {
	if len(results) == 0 {
		return nil
	}
	// Build a lookup map
	resultMap := make(map[int64]SearchResult)
	for _, r := range results {
		resultMap[r.ChunkID] = SearchResult{
			ChunkID: r.ChunkID,
			Score:   r.Score,
		}
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
		`SELECT ch.id, ch.document_id, ch.content, ch.content_zstd, COALESCE(ch.chunk_type, ?), COALESCE(ch.image_path, ''),
		        d.title, d.path, c.name
		 FROM chunks ch
		 JOIN documents d ON d.id = ch.document_id
		 JOIN collections c ON c.id = d.collection_id
		 WHERE ch.id IN (%s)`,
		strings.Join(placeholders, ","),
	)
	args = append([]interface{}{ChunkTypeText}, args...)

	// Push filters into the SQL query so the DB handles filtering
	if filters != nil {
		clause, fargs := filters.ToSQL()
		if clause != "" {
			query += " AND " + clause
			args = append(args, fargs...)
		}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var (
			chunkID     int64
			docID       int64
			content     string
			contentZstd []byte
			chunkType   ChunkType
			imagePath   string
			title       string
			path        string
			collection  string
		)
		if err := rows.Scan(&chunkID, &docID, &content, &contentZstd, &chunkType, &imagePath, &title, &path, &collection); err != nil {
			continue
		}
		if len(contentZstd) > 0 {
			content, err = DecompressString(contentZstd)
			if err != nil {
				continue
			}
		}
		if r, ok := resultMap[chunkID]; ok {
			r.DocumentID = docID
			r.Title = title
			r.Path = path
			r.Collection = collection
			r.Content = content
			r.ChunkType = chunkType
			r.ImagePath = imagePath
			resultMap[chunkID] = r
		}
	}

	// Preserve HNSW ordering
	out := make([]SearchResult, len(results))
	for i, r := range results {
		out[i] = resultMap[r.ChunkID]
	}
	return out
}

func (s *Store) UpdateChunkEmbedding(chunkID int64, embedding []float32) error {
	_, err := s.db.Exec(`UPDATE chunks SET embedding = ? WHERE id = ?`, encodeEmbedding(embedding), chunkID)
	return err
}

// GetChunksWithoutEmbedding returns chunks that don't have embeddings yet.
// If force is true, returns all chunks.
func (s *Store) GetChunksWithoutEmbedding(force bool) ([]Chunk, error) {
	query := `SELECT id, document_id, seq, content, content_zstd, COALESCE(chunk_type, ?), COALESCE(image_path, '') FROM chunks WHERE embedding IS NULL`
	if force {
		query = `SELECT id, document_id, seq, content, content_zstd, COALESCE(chunk_type, ?), COALESCE(image_path, '') FROM chunks`
	}
	rows, err := s.db.Query(query, ChunkTypeText)
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
	return chunks, nil
}

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
	if sim != sim {
		return 0
	}
	return float64(sim)
}
