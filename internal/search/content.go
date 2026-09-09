package search

import (
	"context"
	"strings"
)

// EnrichContent resolves chunk-level hits through the search repository. The
// CLI and MCP surfaces therefore share content loading without reaching into
// SQLite themselves.
func (e *Engine) EnrichContent(ctx context.Context, results []Result) {
	for i := range results {
		if results[i].ChunkID <= 0 {
			results[i].Content = strings.ReplaceAll(results[i].Content, ">>>", "")
			results[i].Content = strings.ReplaceAll(results[i].Content, "<<<", "")
			continue
		}
		if e == nil || e.repository == nil {
			continue
		}
		if content, err := e.repository.GetChunkContent(ctx, results[i].ChunkID); err == nil {
			results[i].Content = content
		}
	}
}
