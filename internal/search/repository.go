package search

import (
	"context"
	"fmt"

	"github.com/ozgurulukir/seek/internal/store"
)

// SearchRepository is the narrow persistence seam consumed by Engine. The
// engine never owns a SQLite handle or constructs SQL; StoreRepository is the
// production adapter and fakes can implement this interface in unit tests.
type SearchRepository interface {
	SearchFTS(ctx context.Context, query string, limit int, filters *FilterSet) ([]Result, error)
	SearchVector(ctx context.Context, query []float32, limit int, filters *FilterSet) ([]Result, error)
	BatchGetFastFields(ctx context.Context, documentIDs []int64, field string) (map[int64]interface{}, error)
	GetChunkContent(ctx context.Context, chunkID int64) (string, error)
	ExecuteAggregation(ctx context.Context, aggregation Aggregation, filters *FilterSet) ([]Bucket, error)
}

// StoreRepository adapts the SQLite store to the search seam. It is kept in
// the search package so command composition has one explicit adapter point.
type StoreRepository struct {
	store *store.Store
}

func NewStoreRepository(s *store.Store) *StoreRepository {
	return &StoreRepository{store: s}
}

func (r *StoreRepository) SearchFTS(ctx context.Context, query string, limit int, filters *FilterSet) ([]Result, error) {
	storeFilters, err := toStoreFilters(filters)
	if err != nil {
		return nil, err
	}
	results, err := r.store.SearchFTSContext(ctx, query, limit, storeFilters)
	return fromStoreResults(results), err
}

func (r *StoreRepository) SearchVector(ctx context.Context, query []float32, limit int, filters *FilterSet) ([]Result, error) {
	storeFilters, err := toStoreFilters(filters)
	if err != nil {
		return nil, err
	}
	results, err := r.store.SearchVectorContext(ctx, query, limit, storeFilters)
	return fromStoreResults(results), err
}

func (r *StoreRepository) BatchGetFastFields(ctx context.Context, documentIDs []int64, field string) (map[int64]interface{}, error) {
	return r.store.FastFields().BatchGetContext(ctx, documentIDs, field)
}

func (r *StoreRepository) GetChunkContent(ctx context.Context, chunkID int64) (string, error) {
	return r.store.GetChunkContentContext(ctx, chunkID)
}

func (r *StoreRepository) ExecuteAggregation(ctx context.Context, aggregation Aggregation, filters *FilterSet) ([]Bucket, error) {
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
	out := make([]Bucket, len(buckets))
	for i, b := range buckets {
		out[i] = Bucket{Key: b.Key, Count: b.Count}
	}
	return out, nil
}

func fromStoreResults(results []store.SearchResult) []Result {
	if results == nil {
		return nil
	}
	out := make([]Result, 0, len(results))
	for _, result := range results {
		out = append(out, Result{
			DocumentID: result.DocumentID,
			ChunkID:    result.ChunkID,
			Seq:        result.Seq,
			Title:      result.Title,
			Path:       result.Path,
			Collection: result.Collection,
			Content:    result.Content,
			Score:      result.Score,
			ChunkType:  ChunkType(result.ChunkType),
			ImagePath:  result.ImagePath,
			StartLine:  result.StartLine,
			EndLine:    result.EndLine,
		})
	}
	return out
}

func toStoreFilters(filters *FilterSet) (*store.FilterSet, error) {
	if filters == nil || len(filters.Items()) == 0 {
		return nil, nil
	}
	result := store.NewFilterSet()
	for _, filter := range filters.Items() {
		switch filter.Kind {
		case FilterCollection:
			result.Add(&store.CollectionFilter{Name: filter.Value})
		case FilterDocType:
			result.Add(&store.DocTypeFilter{Type: filter.Value})
		case FilterLanguage:
			result.Add(&store.FastFieldFilter{Field: "lang", Value: filter.Value})
		case FilterTag:
			result.Add(&store.TagFilter{Tag: filter.Value})
		case FilterRepository:
			result.Add(&store.FastFieldFilter{Field: "repo", Value: filter.Value})
		case FilterDateRange:
			result.Add(&store.DateRangeFilter{After: filter.After, Before: filter.Before})
		case FilterChunkType:
			result.Add(&store.ChunkTypeFilter{Type: int(filter.Chunk)})
		case FilterPath:
			result.Add(&store.PathFilter{Pattern: filter.Pattern})
		case FilterWorkspace:
			result.Add(&store.FastFieldFilter{Field: "workspace", Value: filter.Value})
		default:
			return nil, fmt.Errorf("unsupported search filter kind %q", filter.Kind)
		}
	}
	return result, nil
}
