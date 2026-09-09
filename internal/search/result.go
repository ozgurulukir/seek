package search

// ChunkType identifies the kind of indexed hit without exposing persistence
// package types to the search domain.
type ChunkType int

const (
	ChunkTypeText  ChunkType = 0
	ChunkTypeImage ChunkType = 1
)

// Result is the search-domain representation of a ranked hit. Persistence
// adapters map their storage model into this type at the repository boundary.
type Result struct {
	DocumentID int64
	ChunkID    int64
	Seq        int
	Title      string
	Path       string
	Collection string
	Content    string
	Score      float64
	ChunkType  ChunkType
	ImagePath  string
	StartLine  int
	EndLine    int
}
