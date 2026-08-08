package search

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/store"
)

const (
	DefaultLimit = 20
	RRFk         = 60
)

// Options configures search behavior.
type Options struct {
	// Query is the parsed query AST. If nil, the raw query string is used.
	Query Query
	// Filters to apply to the search.
	Filters *store.FilterSet
	// Aggregations to run alongside the search (spec strings like "type:terms").
	Aggregations []string
	// QueryMode is "raw" or "parsed".
	QueryMode string
	// Limit is the max results.
	Limit int
	// SortBy is the field name to sort by (empty = relevance score).
	SortBy string
	// SortOrder is "asc" or "desc".
	SortOrder string
	// Analyzer is the text analyzer for query-time analysis (tokenization, stemming).
	Analyzer *Analyzer
}

// Engine performs BM25, vector, and hybrid search with optional filters and aggregations.
type Engine struct {
	store       *store.Store
	embedClient *embed.Client
	vlClient    *embed.VLClient
}

func NewEngine(s *store.Store, ec *embed.Client) *Engine {
	return &Engine{store: s, embedClient: ec}
}

// NewEngineWithVL creates a search engine with a VL client for multimodal query embedding.
func NewEngineWithVL(s *store.Store, ec *embed.Client, vlc *embed.VLClient) *Engine {
	return &Engine{store: s, embedClient: ec, vlClient: vlc}
}

// SearchBM25 performs BM25 full-text search with optional filters.
func (e *Engine) SearchBM25(ctx context.Context, query string, limit int, opts Options) ([]store.SearchResult, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}

	ftsQuery := query
	if opts.Query != nil {
		if opts.Analyzer != nil {
			ftsQuery, _ = ToFTS5WithAnalyzer(opts.Query, opts.Analyzer)
		} else {
			ftsQuery, _ = ToFTS5(opts.Query)
		}
	} else if opts.QueryMode != "raw" {
		parsed, err := ParseQuery(query)
		if err == nil && parsed != nil {
			if opts.Analyzer != nil {
				ftsQuery, _ = ToFTS5WithAnalyzer(parsed, opts.Analyzer)
			} else {
				ftsQuery, _ = ToFTS5(parsed)
			}
		}
		// On parse error, fall back to raw query
	}

	results, err := e.store.SearchFTS(ftsQuery, limit, opts.Filters)
	if err != nil {
		return nil, err
	}
	return e.sortResults(results, opts)
}

// SearchVector performs vector semantic search with optional filters.
func (e *Engine) SearchVector(ctx context.Context, query string, limit int, opts Options) ([]store.SearchResult, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}

	// Prefer VL client if available (unified vector space for multimodal)
	var qEmb []float32
	var err error
	if e.vlClient != nil {
		qEmb, err = e.vlClient.EmbedText(query)
		if err != nil {
			return nil, err
		}
	} else if e.embedClient != nil {
		qEmb, err = e.embedClient.EmbedSingle(query)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("vector search requires embedding client")
	}

	results, err := e.store.SearchVector(qEmb, limit, opts.Filters)
	if err != nil {
		return nil, err
	}
	return e.sortResults(results, opts)
}

// SearchHybrid performs hybrid search using RRF fusion with optional filters.
func (e *Engine) SearchHybrid(ctx context.Context, query string, limit int, opts Options) ([]store.SearchResult, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}

	bm25Results, err := e.SearchBM25(ctx, query, limit*2, opts)
	if err != nil {
		return nil, err
	}

	vecResults, err := e.SearchVector(ctx, query, limit*2, opts)
	if err != nil {
		// Fall back to BM25 only if vector search fails
		if len(bm25Results) > limit {
			bm25Results = bm25Results[:limit]
		}
		return bm25Results, nil
	}

	return rrfFusion(bm25Results, vecResults, limit), nil
}

// SearchWithOptions performs search based on the options.
func (e *Engine) SearchWithOptions(ctx context.Context, query string, opts Options) ([]store.SearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = DefaultLimit
	}
	return e.SearchHybrid(ctx, query, opts.Limit, opts)
}

// RunAggregations executes aggregation queries and returns results.
func (e *Engine) RunAggregations(ctx context.Context, specs []string, filters *store.FilterSet) (map[string][]Bucket, error) {
	result := make(map[string][]Bucket)
	for _, spec := range specs {
		agg, err := ParseAggregation(spec)
		if err != nil {
			return nil, fmt.Errorf("parse aggregation %q: %w", spec, err)
		}
		buckets, err := ExecuteAggregation(e.store.DB(), agg)
		if err != nil {
			return nil, fmt.Errorf("execute aggregation %q: %w", spec, err)
		}
		result[spec] = buckets
	}
	return result, nil
}

func rrfFusion(bm25, vec []store.SearchResult, limit int) []store.SearchResult {
	// Key by DocumentID for document-level fusion.
	// BM25 returns ChunkID==0 (document-level), vector returns real chunk IDs.
	// Using docID ensures both branches can merge for the same document.
	scores := make(map[int64]float64)
	resultMap := make(map[int64]store.SearchResult)

	for rank, r := range bm25 {
		scores[r.DocumentID] += 1.0 / float64(RRFk+rank+1)
		resultMap[r.DocumentID] = r
	}

	for rank, r := range vec {
		scores[r.DocumentID] += 1.0 / float64(RRFk+rank+1)
		if _, exists := resultMap[r.DocumentID]; !exists {
			resultMap[r.DocumentID] = r
		}
	}

	// Sort by RRF score
	type scored struct {
		docID int64
		score float64
	}
	var sorted []scored
	for id, s := range scores {
		sorted = append(sorted, scored{id, s})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	if len(sorted) > limit {
		sorted = sorted[:limit]
	}

	results := make([]store.SearchResult, len(sorted))
	for i, s := range sorted {
		r := resultMap[s.docID]
		r.Score = s.score
		results[i] = r
	}

	return results
}

// sortResults sorts search results by a fast field if specified.
func (e *Engine) sortResults(results []store.SearchResult, opts Options) ([]store.SearchResult, error) {
	if opts.SortBy == "" || len(results) == 0 {
		return results, nil
	}

	// Fetch fast field values for all result document IDs
	docIDs := make([]int64, len(results))
	for i, r := range results {
		docIDs[i] = r.DocumentID
	}

	values, err := e.store.FastFields().BatchGet(docIDs, opts.SortBy)
	if err != nil {
		// If fast field doesn't exist, return unsorted
		return results, nil
	}

	// Sort results by fast field value
	ascending := opts.SortOrder != "desc"
	sort.Slice(results, func(i, j int) bool {
		vi, okI := values[results[i].DocumentID]
		vj, okJ := values[results[j].DocumentID]
		if !okI && !okJ {
			return i < j // preserve original order
		}
		if !okI {
			return false // missing values go last
		}
		if !okJ {
			return true // missing values go last
		}

		cmp := compareFastFieldValues(vi, vj)
		if cmp == 0 {
			return i < j // preserve original order for ties
		}
		if ascending {
			return cmp < 0
		}
		return cmp > 0
	})

	return results, nil
}

// compareFastFieldValues compares two fast field values.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareFastFieldValues(a, b interface{}) int {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		if !ok {
			return 1
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
		return 0
	case float64:
		bv, ok := b.(float64)
		if !ok {
			return 1
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
		return 0
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return 1
		}
		af, aerr := av.Float64()
		bf, berr := bv.Float64()
		if aerr != nil || berr != nil {
			// Fall back to string comparison
			if av.String() < bv.String() {
				return -1
			}
			if av.String() > bv.String() {
				return 1
			}
			return 0
		}
		if af < bf {
			return -1
		}
		if af > bf {
			return 1
		}
		return 0
	default:
		// Fallback: string comparison
		as := fmt.Sprintf("%v", a)
		bs := fmt.Sprintf("%v", b)
		if as < bs {
			return -1
		}
		if as > bs {
			return 1
		}
		return 0
	}
}
