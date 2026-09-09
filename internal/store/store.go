package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"

	"github.com/ozgurulukir/seek/internal/config"
)

type Store struct {
	db                 *sql.DB
	vectorIndex        VectorIndex
	fastFields         *FastFieldStore
	compressionEnabled bool
	compressionLevel   int
	closeOnce          sync.Once
	closeErr           error
}

func Open(dbPath string) (*Store, error) {
	// The index holds the searchable text of the user's notes, conversations,
	// and code — a private file. Databases created by older seek versions are
	// 0644 (umask-dependent); tighten on every open (idempotent, no-op when
	// already 0600). Best effort: a read-only mount must not break startup.
	if fi, err := os.Stat(dbPath); err == nil && !fi.IsDir() {
		_ = os.Chmod(dbPath, config.DefaultPrivateFilePerms)
	}
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
	s.closeOnce.Do(func() {
		var errs []error
		if err := s.FlushVectorIndex(context.Background()); err != nil {
			errs = append(errs, err)
		}
		if s.db != nil {
			if err := s.db.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close database: %w", err))
			}
		}
		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}

// FlushVectorIndex publishes the vector graph and its manifest before the
// database is closed. The generation is calculated from the persisted
// embeddings so a restart can reject a stale graph instead of returning ghost
// vector hits.
func (s *Store) FlushVectorIndex(ctx context.Context) error {
	if s.vectorIndex == nil {
		return nil
	}
	var errs []error
	if metadata, ok := s.vectorIndex.(VectorIndexMetadata); ok {
		generation, err := s.vectorGenerationContext(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("calculate vector generation: %w", err))
		} else {
			metadata.SetManifestGeneration(generation)
		}
	}
	if flusher, ok := s.vectorIndex.(VectorIndexFlusher); ok {
		if err := flusher.Flush(); err != nil {
			errs = append(errs, fmt.Errorf("flush vector index: %w", err))
		}
	}
	return errors.Join(errs...)
}

// SetVectorIndex sets the vector index backend (HNSW or linear scan).
func (s *Store) SetVectorIndex(idx VectorIndex) {
	s.vectorIndex = idx
}

// RecoverVectorIndex validates the loaded manifest against the current
// persisted embeddings. A mismatch is a safe stale state: clear the graph,
// rebuild from SQLite, and expose the repair through the warning interface.
func (s *Store) RecoverVectorIndex(ctx context.Context) error {
	metadata, ok := s.vectorIndex.(VectorIndexMetadata)
	if !ok || metadata.ManifestGeneration() == "" {
		return nil
	}
	current, err := s.vectorGenerationContext(ctx)
	if err != nil {
		return fmt.Errorf("read vector generation: %w", err)
	}
	if current == metadata.ManifestGeneration() {
		return nil
	}
	if err := s.vectorIndex.Clear(); err != nil {
		return fmt.Errorf("clear stale vector index: %w", err)
	}
	metadata.SetWarning(fmt.Sprintf("vector index rebuilt because persisted embeddings changed (generation %s -> %s)", metadata.ManifestGeneration(), current))
	if _, err := s.syncVectorIndexFullContext(ctx); err != nil {
		return fmt.Errorf("rebuild stale vector index: %w", err)
	}
	return nil
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

// ConfigureCompression applies the persisted compression setting using one
// consistent interpretation of the empty and "none" values.
func (s *Store) ConfigureCompression(cfg config.CompressionConfig) {
	algorithm := strings.ToLower(strings.TrimSpace(cfg.Algorithm))
	s.SetCompression(algorithm != "" && algorithm != "none", cfg.Level)
}

// SyncVectorIndex adds all embedded chunks to the vector index.
// It clears the index first to ensure consistency — otherwise repeated
// syncs (e.g. after each `seek embed`) would accumulate duplicate/stale
// entries and grow the HNSW graph indefinitely.
func (s *Store) migrate() error {
	// Verify FTS5 is available (requires build tag: -tags "fts5")
	var fts5ok int
	if err := s.db.QueryRow(`SELECT 1 FROM pragma_compile_options WHERE compile_options = 'ENABLE_FTS5'`).Scan(&fts5ok); err != nil {
		return fmt.Errorf("SQLite FTS5 not enabled. Build with: make build (or: go build -tags \"fts5\")")
	}

	if err := s.initCoreSchema(); err != nil {
		return err
	}

	s.applyAlterStatements()

	if err := s.initFTS(); err != nil {
		return err
	}

	return nil
}

func (s *Store) initCoreSchema() error {
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
	return nil
}

func (s *Store) applyAlterStatements() {
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
}

func (s *Store) execIgnoreDuplicate(stmt string) {
	_, err := s.db.Exec(stmt)
	if err != nil {
		errMsg := err.Error()
		// SQLite returns "duplicate column name" when column already exists
		if strings.Contains(errMsg, "duplicate column") {
			return
		}
		// Log unexpected ALTER TABLE errors so they are not silently lost.
		fmt.Fprintf(os.Stderr, "WARN: migration statement failed: %v\n  SQL: %s\n", err, stmt)
	}
}
