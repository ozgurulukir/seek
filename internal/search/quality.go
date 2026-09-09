package search

import (
	"strings"

	"github.com/ozgurulukir/seek/internal/store"
)

// ContentKind classifies the Content field of a search result for JSON/MCP
// consumers. Chunk-level hits (from the vector leg) carry the complete chunk
// text; document-level BM25/hybrid hits carry a 40-token FTS snippet.
//
// This is the single source of truth for the "full" vs "snippet" distinction
// that the CLI and MCP surfaces each used to reimplement.
func ContentKind(r store.SearchResult) string {
	if r.ChunkID > 0 {
		return "full"
	}
	return "snippet"
}

// EnrichContent is a compatibility helper for callers that still own a Store.
// Production search surfaces should use Engine.EnrichContent so content reads
// remain behind the repository seam.
//
// It normalizes result content for JSON/MCP output:
//
//   - chunk-level hits (ChunkID > 0) are replaced with their full stored
//     chunk content via GetChunkContent;
//   - document-level hits keep their FTS snippet with the >>> / <<< highlight
//     markers stripped.
//
// Best-effort: a missing chunk keeps its snippet (with markers stripped), and
// a nil db leaves chunk-level content untouched. The db may be nil in tests.
func EnrichContent(db *store.Store, results []store.SearchResult) {
	for i := range results {
		if results[i].ChunkID <= 0 {
			results[i].Content = strings.ReplaceAll(results[i].Content, ">>>", "")
			results[i].Content = strings.ReplaceAll(results[i].Content, "<<<", "")
			continue
		}
		if db == nil {
			continue
		}
		if content, err := db.GetChunkContent(results[i].ChunkID); err == nil {
			results[i].Content = content
		}
	}
}
