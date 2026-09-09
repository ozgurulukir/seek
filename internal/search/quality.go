package search

// ContentKind classifies the Content field of a search result for JSON/MCP
// consumers. Chunk-level hits (from the vector leg) carry the complete chunk
// text; document-level BM25/hybrid hits carry a 40-token FTS snippet.
//
// This is the single source of truth for the "full" vs "snippet" distinction
// that the CLI and MCP surfaces each used to reimplement.
func ContentKind(r Result) string {
	if r.ChunkID > 0 {
		return "full"
	}
	return "snippet"
}
