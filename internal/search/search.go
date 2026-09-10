package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/ozgurulukir/seek/internal/embed"
)

const (
	DefaultLimit = 20
	DefaultRRFK  = 60
	RRFk         = DefaultRRFK // Deprecated: use DefaultRRFK or Options.RRFK
)

// Options configures search behavior.
type Options struct {
	// Query is the parsed query AST. If nil, the raw query string is used.
	Query Query
	// Filters to apply to the search.
	Filters *FilterSet
	// Aggregations to run alongside the search (spec strings like "type:terms").
	Aggregations []string
	// QueryMode is "raw" or "parsed".
	QueryMode string
	// Limit is the max results.
	Limit int
	// RRFK is the Reciprocal Rank Fusion k constant. If <= 0, DefaultRRFK (60) is used.
	RRFK int
	// SortBy is the field name to sort by (empty = relevance score).
	SortBy string
	// SortOrder is "asc" or "desc".
	SortOrder string
	// Analyzer is the text analyzer for query-time analysis (tokenization, stemming).
	Analyzer *Analyzer
}

// Logger matches indexer.Logger for uniform diagnostic output.
type Logger interface {
	Printf(format string, v ...interface{})
}

// Engine performs BM25, vector, and hybrid search with optional filters, aggregations, and reranking.
type Engine struct {
	repository  SearchRepository
	embedClient embed.QueryEmbedder
	vlClient    embed.VLQueryEmbedder
	reranker    embed.Reranker
	logger      Logger
}

func NewEngine(repository SearchRepository, ec embed.QueryEmbedder) *Engine {
	provider := (embed.Provider{Query: ec}).NormalizeCapabilities()
	return &Engine{repository: repository, embedClient: provider.Query}
}

// NewEngineWithVL creates a search engine with a VL client for multimodal query embedding.
func NewEngineWithVL(repository SearchRepository, ec embed.QueryEmbedder, vlc embed.VLQueryEmbedder) *Engine {
	provider := (embed.Provider{Query: ec, VLQuery: vlc}).NormalizeCapabilities()
	return &Engine{
		repository:  repository,
		embedClient: provider.Query,
		vlClient:    provider.VLQuery,
	}
}

// NewEngineWithProvider builds the engine from the runtime-owned capability
// bundle, keeping provider construction out of commands and search logic.
func NewEngineWithProvider(repository SearchRepository, provider embed.Provider) *Engine {
	provider = provider.NormalizeCapabilities()
	e := NewEngine(repository, provider.Query)
	e.vlClient = provider.VLQuery
	e.reranker = provider.Reranker
	return e
}

// WithReranker sets an optional cross-encoder reranker.
func (e *Engine) WithReranker(r embed.Reranker) *Engine {
	e.reranker = r
	return e
}

// WithLogger attaches a logger for diagnostic warnings and info messages.
func (e *Engine) WithLogger(l Logger) *Engine {
	e.logger = l
	return e
}

// renderFTS5 converts an AST Query to an FTS5 query string using the optional analyzer.
func renderFTS5(q Query, a *Analyzer) string {
	if q == nil {
		return ""
	}
	if a != nil {
		fts, _ := ToFTS5WithAnalyzer(q, a)
		return fts
	}
	fts, _ := ToFTS5(q)
	return fts
}

// searchBM25Raw executes the BM25 full-text query against FTS5 and returns raw candidate hits.
func (e *Engine) searchBM25Raw(ctx context.Context, query string, limit int, opts Options) ([]Result, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}

	ftsQuery := query
	if opts.Query != nil {
		ftsQuery = renderFTS5(opts.Query, opts.Analyzer)
	} else if opts.QueryMode != "raw" {
		parsed, err := ParseQuery(query)
		if err == nil && parsed != nil {
			ftsQuery = renderFTS5(parsed, opts.Analyzer)
		}
		// On parse error, fall back to raw query
	}

	return e.repository.SearchFTS(ctx, ftsQuery, limit, opts.Filters)
}

// searchVectorRaw executes the vector semantic query and returns raw candidate hits.
func (e *Engine) searchVectorRaw(ctx context.Context, query string, limit int, opts Options) ([]Result, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}

	// Prefer VL client if available (unified vector space for multimodal)
	var qEmb []float32
	var err error
	if e.vlClient != nil {
		if contextual, ok := e.vlClient.(embed.ContextVLQueryEmbedder); ok {
			qEmb, err = contextual.EmbedTextContext(ctx, query)
		} else {
			qEmb, err = e.vlClient.EmbedText(query)
		}
		if err != nil {
			return nil, err
		}
	} else if e.embedClient != nil {
		if contextual, ok := e.embedClient.(embed.ContextQueryEmbedder); ok {
			qEmb, err = contextual.EmbedQueryContext(ctx, query)
		} else {
			qEmb, err = e.embedClient.EmbedQuery(query)
		}
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("vector search requires embedding client")
	}

	return e.repository.SearchVector(ctx, qEmb, limit, opts.Filters)
}

// SearchBM25 performs BM25 full-text search with optional filters, reranking, and sorting.
func (e *Engine) SearchBM25(ctx context.Context, query string, limit int, opts Options) ([]Result, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}

	rawLimit := limit * 2
	if e.reranker != nil {
		rawLimit = limit * 3
	}

	results, err := e.searchBM25Raw(ctx, query, rawLimit, opts)
	if err != nil {
		return nil, err
	}

	if opts.SortBy != "" {
		sorted, err := e.sortResults(ctx, results, opts)
		if err != nil {
			return nil, err
		}
		if len(sorted) > limit {
			sorted = sorted[:limit]
		}
		return sorted, nil
	}

	reranked, err := e.rerankResults(ctx, query, results, limit)
	return reranked, err
}

// SearchVector performs vector semantic search with optional filters, reranking, and sorting.
func (e *Engine) SearchVector(ctx context.Context, query string, limit int, opts Options) ([]Result, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}

	rawLimit := limit * 2
	if e.reranker != nil {
		rawLimit = limit * 3
	}

	results, err := e.searchVectorRaw(ctx, query, rawLimit, opts)
	if err != nil {
		return nil, err
	}

	if opts.SortBy != "" {
		sorted, err := e.sortResults(ctx, results, opts)
		if err != nil {
			return nil, err
		}
		if len(sorted) > limit {
			sorted = sorted[:limit]
		}
		return sorted, nil
	}

	reranked, err := e.rerankResults(ctx, query, results, limit)
	return reranked, err
}

// SearchHybrid performs hybrid search using RRF fusion with optional filters, reranking, and sorting.
func (e *Engine) SearchHybrid(ctx context.Context, query string, limit int, opts Options) ([]Result, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}

	candidateLimit := limit * 2
	if e.reranker != nil {
		candidateLimit = limit * 3
	}

	bm25Results, bm25Err := e.searchBM25Raw(ctx, query, candidateLimit, opts)
	vecResults, vecErr := e.searchVectorRaw(ctx, query, candidateLimit, opts)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if errors.Is(bm25Err, context.Canceled) || errors.Is(bm25Err, context.DeadlineExceeded) {
		return nil, bm25Err
	}
	if errors.Is(vecErr, context.Canceled) || errors.Is(vecErr, context.DeadlineExceeded) {
		return nil, vecErr
	}

	if bm25Err != nil && vecErr != nil {
		return nil, fmt.Errorf("hybrid search failed: bm25: %v; vector: %w", bm25Err, vecErr)
	}

	var fused []Result
	if bm25Err != nil {
		if e.logger != nil {
			e.logger.Printf("  WARN: hybrid search BM25 leg failed: %v\n", bm25Err)
		}
		// Fall back to Vector candidates (keep full candidate pool for reranker)
		fused = vecResults
	} else if vecErr != nil {
		if e.logger != nil {
			e.logger.Printf("  WARN: hybrid search vector leg failed: %v\n", vecErr)
		}
		// Fall back to BM25 candidates (keep full candidate pool for reranker)
		fused = bm25Results
	} else {
		fused = rrfFusionWithK(bm25Results, vecResults, candidateLimit, opts.RRFK)
	}

	if opts.SortBy != "" {
		sorted, err := e.sortResults(ctx, fused, opts)
		if err != nil {
			return nil, err
		}
		if len(sorted) > limit {
			sorted = sorted[:limit]
		}
		return sorted, nil
	}

	reranked, err := e.rerankResults(ctx, query, fused, limit)
	return reranked, err
}

// SearchWithOptions performs search based on the options.
func (e *Engine) SearchWithOptions(ctx context.Context, query string, opts Options) ([]Result, error) {
	if opts.Limit <= 0 {
		opts.Limit = DefaultLimit
	}
	return e.SearchHybrid(ctx, query, opts.Limit, opts)
}

// RunAggregations executes aggregation queries and returns results.
func (e *Engine) RunAggregations(ctx context.Context, specs []string, filters *FilterSet) (map[string][]Bucket, error) {
	result := make(map[string][]Bucket)
	for _, spec := range specs {
		agg, err := ParseAggregation(spec)
		if err != nil {
			return nil, fmt.Errorf("parse aggregation %q: %w", spec, err)
		}
		buckets, err := e.repository.ExecuteAggregation(ctx, agg, filters)
		if err != nil {
			return nil, fmt.Errorf("execute aggregation %q: %w", spec, err)
		}
		result[spec] = buckets
	}
	return result, nil
}

// sortResults sorts search results by a fast field if specified.
func (e *Engine) sortResults(ctx context.Context, results []Result, opts Options) ([]Result, error) {
	if opts.SortBy == "" || len(results) == 0 {
		return results, nil
	}

	// Fetch fast field values for all result document IDs
	docIDs := make([]int64, len(results))
	for i, r := range results {
		docIDs[i] = r.DocumentID
	}

	values, err := e.repository.BatchGetFastFields(ctx, docIDs, opts.SortBy)
	if err != nil {
		return nil, fmt.Errorf("sort by %q: %w", opts.SortBy, err)
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
