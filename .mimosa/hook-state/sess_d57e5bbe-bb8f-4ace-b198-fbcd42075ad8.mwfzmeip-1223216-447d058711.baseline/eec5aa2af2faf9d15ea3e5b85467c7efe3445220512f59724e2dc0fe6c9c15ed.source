package search

import (
	"context"
	"fmt"
	"strings"
)

// EnrichContent resolves chunk-level hits through the search repository. The
// CLI and MCP surfaces therefore share content loading without reaching into
// SQLite themselves.
func (e *Engine) EnrichContent(ctx context.Context, results []Result) error {
	for i := range results {
		if results[i].ChunkID <= 0 {
			results[i].Content = strings.ReplaceAll(results[i].Content, ">>>", "")
			results[i].Content = strings.ReplaceAll(results[i].Content, "<<<", "")
			continue
		}
		if e == nil || e.repository == nil {
			continue
		}
		content, err := e.repository.GetChunkContent(ctx, results[i].ChunkID)
		if err != nil {
			return fmt.Errorf("enrich chunk %d: %w", results[i].ChunkID, err)
		}
		results[i].Content = content
	}
	return nil
}
