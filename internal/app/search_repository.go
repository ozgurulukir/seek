package app

import (
	"context"
	"fmt"

	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

// StoreSearchRepository is the composition-root adapter between search's
// persistence-neutral contract and SQLite. Keeping it here prevents the
// search domain package from importing Store types or SQL concerns.
type StoreSearchRepository struct {
	store *store.Store
}

func NewStoreSearchRepository(s *store.Store) *StoreSearchRepository {
	return &StoreSearchRepository{store: s}
}

func (r *StoreSearchRepository) SearchFTS(ctx context.Context, query string, limit int, filters *search.FilterSet) ([]search.Result, error) {
	storeFilters, err := toStoreFilters(filters)
	if err != nil {
		return nil, err
	}
	results, err := r.store.SearchFTSContext(ctx, query, limit, storeFilters)
	return fromStoreResults(results), err
}

func (r *StoreSearchRepository) SearchVector(ctx context.Context, query []float32, limit int, filters *search.FilterSet) ([]search.Result, error) {
	storeFilters, err := toStoreFilters(filters)
	if err != nil {
		return nil, err
	}
	results, err := r.store.SearchVectorContext(ctx, query, limit, storeFilters)
	return fromStoreResults(results), err
}

func (r *StoreSearchRepository) BatchGetFastFields(ctx context.Context, documentIDs []int64, field string) (map[int64]interface{}, error) {
	return r.store.FastFields().BatchGetContext(ctx, documentIDs, field)
}

func (r *StoreSearchRepository) GetChunkContent(ctx context.Context, chunkID int64) (string, error) {
	return r.store.GetChunkContentContext(ctx, chunkID)
}

func (r *StoreSearchRepository) ExecuteAggregation(ctx context.Context, aggregation search.Aggregation, filters *search.FilterSet) ([]search.Bucket, error) {
	storeFilters, err := toStoreFilters(filters)
	if err != nil {
		return nil, err
	}
	spec := aggregation.Spec()
	buckets, err := r.store.ExecuteAggregationContext(ctx, store.AggregationSpec{
		Type:     string(spec.Type),
		Field:    spec.Field,
		Interval: spec.Interval,
		Ranges:   spec.Ranges,
	}, storeFilters)
	if err != nil {
		return nil, err
	}
	out := make([]search.Bucket, len(buckets))
	for i, bucket := range buckets {
		out[i] = search.Bucket{Key: bucket.Key, Count: bucket.Count}
	}
	return out, nil
}

func fromStoreResults(results []store.SearchResult) []search.Result {
	if results == nil {
		return nil
	}
	out := make([]search.Result, 0, len(results))
	for _, result := range results {
		out = append(out, search.Result{
			DocumentID: result.DocumentID,
			ChunkID:    result.ChunkID,
			Seq:        result.Seq,
			Title:      result.Title,
			Path:       result.Path,
			Collection: result.Collection,
			Content:    result.Content,
			Score:      result.Score,
			ChunkType:  search.ChunkType(result.ChunkType),
			ImagePath:  result.ImagePath,
			StartLine:  result.StartLine,
			EndLine:    result.EndLine,
		})
	}
	return out
}

func toStoreFilters(filters *search.FilterSet) (*store.FilterSet, error) {
	if filters == nil || len(filters.Items()) == 0 {
		return nil, nil
	}
	result := store.NewFilterSet()
	for _, filter := range filters.Items() {
		switch filter.Kind {
		case search.FilterCollection:
			result.Add(&store.CollectionFilter{Name: filter.Value})
		case search.FilterDocType:
			result.Add(&store.DocTypeFilter{Type: filter.Value})
		case search.FilterLanguage:
			result.Add(&store.FastFieldFilter{Field: "lang", Value: filter.Value})
		case search.FilterTag:
			result.Add(&store.TagFilter{Tag: filter.Value})
		case search.FilterRepository:
			result.Add(&store.FastFieldFilter{Field: "repo", Value: filter.Value})
		case search.FilterDateRange:
			result.Add(&store.DateRangeFilter{After: filter.After, Before: filter.Before})
		case search.FilterChunkType:
			result.Add(&store.ChunkTypeFilter{Type: int(filter.Chunk)})
		case search.FilterPath:
			result.Add(&store.PathFilter{Pattern: filter.Pattern})
		case search.FilterWorkspace:
			result.Add(&store.FastFieldFilter{Field: "workspace", Value: filter.Value})
		default:
			return nil, fmt.Errorf("unsupported search filter kind %q", filter.Kind)
		}
	}
	return result, nil
}
