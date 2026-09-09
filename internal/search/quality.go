package search

import (
	"cmp"
	"context"
	"fmt"
	"slices"
)

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

// rrfFusion combines lexical and semantic candidate lists at document level.
// Keeping fusion and reranking together makes result-quality policy explicit
// and keeps the search orchestration focused on repository calls.
func rrfFusion(bm25, vec []Result, limit int) []Result {
	return rrfFusionWithK(bm25, vec, limit, DefaultRRFK)
}

func rrfFusionWithK(bm25, vec []Result, limit int, k int) []Result {
	if k <= 0 {
		k = DefaultRRFK
	}
	scores := make(map[int64]float64)
	resultMap := make(map[int64]Result)
	for rank, r := range bm25 {
		scores[r.DocumentID] += 1.0 / float64(k+rank+1)
		resultMap[r.DocumentID] = r
	}
	for rank, r := range vec {
		scores[r.DocumentID] += 1.0 / float64(k+rank+1)
		if _, exists := resultMap[r.DocumentID]; !exists {
			resultMap[r.DocumentID] = r
		}
	}

	type scored struct {
		docID int64
		score float64
	}
	sorted := make([]scored, 0, len(scores))
	for id, score := range scores {
		sorted = append(sorted, scored{id, score})
	}
	slices.SortFunc(sorted, func(a, b scored) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		return cmp.Compare(a.docID, b.docID)
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	results := make([]Result, len(sorted))
	for i, item := range sorted {
		result := resultMap[item.docID]
		result.Score = item.score
		results[i] = result
	}
	return results
}

// rerankResults re-scores candidate search results using the configured
// cross-encoder. Reranker failures are returned so callers never silently
// downgrade a requested quality stage.
func (e *Engine) rerankResults(ctx context.Context, query string, results []Result, limit int) ([]Result, error) {
	if e.reranker == nil || len(results) <= 1 {
		if len(results) > limit {
			return results[:limit], nil
		}
		return results, nil
	}

	docTexts := make([]string, len(results))
	for i, result := range results {
		docTexts[i] = result.Title + "\n" + result.Content
	}
	rerankScores, err := e.reranker.Rerank(ctx, query, docTexts, limit)
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}
	if len(rerankScores) == 0 {
		if len(results) > limit {
			return results[:limit], nil
		}
		return results, nil
	}

	reranked := make([]Result, 0, len(rerankScores))
	for _, score := range rerankScores {
		if score.Index >= 0 && score.Index < len(results) {
			result := results[score.Index]
			result.Score = score.RelevanceScore
			reranked = append(reranked, result)
		}
	}
	return reranked, nil
}
