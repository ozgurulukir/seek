package store

// Domain models shared across the store package. Extracted from store.go as
// part of the god-object decomposition — a mechanical move, no changes.

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
//
// CAUTION: Changing this constant causes migrate() to detect a tokenizer change
// via sqlite_master on next DB open, triggering an automatic DROP, CREATE, and
// full repopulation of documents_fts from chunk contents. Do not change casually.
const FTSTokenize = "unicode61 remove_diacritics 2"

// FTSTitleWeight is the bm25 column weight applied to the title column (10x),
// boosting title matches over body matches. Content stays at the default 1.0.
const FTSTitleWeight = 10.0

type ChunkType int

const (
	ChunkTypeText  ChunkType = 0
	ChunkTypeImage ChunkType = 1
)

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
