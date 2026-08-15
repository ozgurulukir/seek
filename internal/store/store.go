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
	CollectionTypeMarkdown  CollectionType = "markdown"
	CollectionTypeClaude    CollectionType = "claude"
	CollectionTypeCodex     CollectionType = "codex"
	CollectionTypeImages    CollectionType = "images"
	CollectionTypePDF       CollectionType = "pdf"
	CollectionTypeParser    CollectionType = "parser"
	CollectionTypeDocuments CollectionType = "documents"
	CollectionTypeCode      CollectionType = "code"
)

// FTSTokenize is the FTS5 unicode61 tokenizer configuration.
// remove_diacritics 2 enables full Unicode case-folding (incl. Turkish İ/ı,
// ç/ğ/ş/ü/ö), so non-ASCII terms are indexed and queried consistently.
// Stored as a constant because FTS5 tokenizer params are fixed at CREATE time.
const FTSTokenize = "unicode61 remove_diacritics 2"

// FTSTitleWeight is the bm25 column weight applied to the title column (10x),
// boosting title matches over body matches. Content stays at the default 1.0.
const FTSTitleWeight = 10.0

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
	cleaned := filepath.ToSlash(filepath.Clean(f.Pattern))
	return "replace(d.path, '\\', '/') GLOB ?", []interface{}{cleaned}
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
	Backend       string // extractor backend override for this collection ("" = use config default)
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
	StartLine  int
	EndLine    int
	CreatedAt  string
}

type SearchResult struct {
	DocumentID int64
	ChunkID    int64
	Seq        int
	Title      string
	Path       string
	Collection string
	Content    string
	Score      float64
	ChunkType  ChunkType
	ImagePath  string // non-empty for image chunks
	StartLine  int
	EndLine    int
}

func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
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
// It clears the index first to ensure consistency — otherwise repeated
// syncs (e.g. after each `seek embed`) would accumulate duplicate/stale
// entries and grow the HNSW graph indefinitely.
func (s *Store) SyncVectorIndex() error {
	if s.vectorIndex == nil {
		return nil
	}

	// Clear existing entries so we rebuild from the current DB state.
	if err := s.vectorIndex.Clear(); err != nil {
		return fmt.Errorf("clear vector index: %w", err)
	}

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
		`ALTER TABLE collections ADD COLUMN backend TEXT`,
		`ALTER TABLE chunks ADD COLUMN start_line INTEGER DEFAULT 0`,
		`ALTER TABLE chunks ADD COLUMN end_line INTEGER DEFAULT 0`,
	}
	for _, stmt := range alterStmts {
		s.execIgnoreDuplicate(stmt)
	}

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
	return cols, nil
}

func (s *Store) DeleteCollection(id int64) error {
	// Delete fast_fields for documents in this collection
	_, _ = s.db.Exec(`DELETE FROM fast_fields WHERE doc_id IN (SELECT id FROM documents WHERE collection_id = ?)`, id)
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
		`SELECT id, collection_id, path, title, content_hash, mtime, line_count, created_at, updated_at
		 FROM documents WHERE collection_id = ? AND path = ?`,
		collectionID, path,
	).Scan(&d.ID, &d.CollectionID, &d.Path, &d.Title, &d.ContentHash, &d.Mtime, &d.LineCount, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) UpsertDocument(collectionID int64, path, title, contentHash string, mtime float64, lineCount int) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var id int64
	err := s.db.QueryRow(
		`INSERT INTO documents (collection_id, path, title, content_hash, mtime, line_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(collection_id, path) DO UPDATE SET
		   title = excluded.title,
		   content_hash = excluded.content_hash,
		   mtime = excluded.mtime,
		   line_count = excluded.line_count,
		   updated_at = excluded.updated_at
		 RETURNING id`,
		collectionID, path, title, contentHash, mtime, lineCount, now, now,
	).Scan(&id)
	if err != nil {
		return 0, err
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

// DeleteDocument removes a document and its chunks/FTS/fast_field entries.
func (s *Store) DeleteDocument(docID int64) error {
	_, _ = s.db.Exec(`DELETE FROM fast_fields WHERE doc_id = ?`, docID)
	if _, err := s.db.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID); err != nil {
		return fmt.Errorf("delete fts entry: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM chunks WHERE document_id = ?`, docID); err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
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
	if _, err := s.db.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID); err != nil {
		return fmt.Errorf("delete fts entry: %w", err)
	}
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
	if _, err := s.db.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID); err != nil {
		return fmt.Errorf("delete fts entry: %w", err)
	}
	_, err = s.db.Exec(`INSERT INTO documents_fts (rowid, title, content) VALUES (?, ?, ?)`, docID, existingTitle, combined)
	return err
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
		clause, fargs := filters.ToSQL()
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
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.DocumentID, &r.Title, &r.Path, &r.Collection, &r.Content, &r.Score); err != nil {
			return nil, err
		}
		// Populate line numbers from first chunk if available
		var sLine, eLine int
		if err := s.db.QueryRow(`SELECT COALESCE(start_line, 0), COALESCE(end_line, 0) FROM chunks WHERE document_id = ? ORDER BY seq ASC LIMIT 1`, r.DocumentID).Scan(&sLine, &eLine); err == nil {
			r.StartLine = sLine
			r.EndLine = eLine
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
	sqlQuery := `SELECT ch.id, ch.document_id, ch.seq, ch.content, ch.content_zstd, ch.embedding,
		        COALESCE(ch.chunk_type, ?), COALESCE(ch.image_path, ''),
		        COALESCE(ch.start_line, 0), COALESCE(ch.end_line, 0),
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
			seq         int
			content     string
			contentZstd []byte
			embBlob     []byte
			chunkType   ChunkType
			imagePath   string
			startLine   int
			endLine     int
			title       string
			path        string
			collection  string
		)
		if err := rows.Scan(&chunkID, &docID, &seq, &content, &contentZstd, &embBlob, &chunkType, &imagePath, &startLine, &endLine, &title, &path, &collection); err != nil {
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
				Seq:        seq,
				Title:      title,
				Path:       path,
				Collection: collection,
				Content:    content,
				ChunkType:  chunkType,
				ImagePath:  imagePath,
				StartLine:  startLine,
				EndLine:    endLine,
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
		`SELECT ch.id, ch.document_id, ch.seq, ch.content, ch.content_zstd, COALESCE(ch.chunk_type, ?), COALESCE(ch.image_path, ''),
		        COALESCE(ch.start_line, 0), COALESCE(ch.end_line, 0),
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
			seq         int
			content     string
			contentZstd []byte
			chunkType   ChunkType
			imagePath   string
			startLine   int
			endLine     int
			title       string
			path        string
			collection  string
		)
		if err := rows.Scan(&chunkID, &docID, &seq, &content, &contentZstd, &chunkType, &imagePath, &startLine, &endLine, &title, &path, &collection); err != nil {
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
			r.Seq = seq
			r.Title = title
			r.Path = path
			r.Collection = collection
			r.Content = content
			r.ChunkType = chunkType
			r.ImagePath = imagePath
			r.StartLine = startLine
			r.EndLine = endLine
			resultMap[chunkID] = r
		}
	}

	// Preserve HNSW ordering while excluding chunks that failed filters or were deleted
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if res, ok := resultMap[r.ChunkID]; ok && res.DocumentID != 0 {
			out = append(out, res)
		}
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
