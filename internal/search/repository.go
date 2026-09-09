package search

import (
	"context"

	"github.com/ozgurulukir/seek/internal/store"
)

// SearchRepository is the narrow persistence seam consumed by Engine. The
// engine never owns a SQLite handle or constructs SQL; StoreRepository is the
// production adapter and fakes can implement this interface in unit tests.
type SearchRepository interface {
	SearchFTS(ctx context.Context, query string, limit int, filters *store.FilterSet) ([]store.SearchResult, error)
	SearchVector(ctx context.Context, query []float32, limit int, filters *store.FilterSet) ([]store.SearchResult, error)
	BatchGetFastFields(ctx context.Context, documentIDs []int64, field string) (map[int64]interface{}, error)
	GetChunkContent(ctx context.Context, chunkID int64) (string, error)
	ExecuteAggregation(ctx context.Context, aggregation Aggregation, filters *store.FilterSet) ([]Bucket, error)
}

// StoreRepository adapts the SQLite store to the search seam. It is kept in
// the search package so command composition has one explicit adapter point.
type StoreRepository struct {
	store *store.Store
}

func NewStoreRepository(s *store.Store) *StoreRepository {
	return &StoreRepository{store: s}
}

func (r *StoreRepository) SearchFTS(ctx context.Context, query string, limit int, filters *store.FilterSet) ([]store.SearchResult, error) {
	return r.store.SearchFTSContext(ctx, query, limit, filters)
}

func (r *StoreRepository) SearchVector(ctx context.Context, query []float32, limit int, filters *store.FilterSet) ([]store.SearchResult, error) {
	return r.store.SearchVectorContext(ctx, query, limit, filters)
}

func (r *StoreRepository) BatchGetFastFields(ctx context.Context, documentIDs []int64, field string) (map[int64]interface{}, error) {
	return r.store.FastFields().BatchGetContext(ctx, documentIDs, field)
}

func (r *StoreRepository) GetChunkContent(ctx context.Context, chunkID int64) (string, error) {
	return r.store.GetChunkContentContext(ctx, chunkID)
}

func (r *StoreRepository) ExecuteAggregation(ctx context.Context, aggregation Aggregation, filters *store.FilterSet) ([]Bucket, error) {
	query, args := aggregation.SQL()
	_, countOnly := aggregation.(*CountAggregation)
	buckets, err := r.store.ExecuteAggregationContext(ctx, query, args, filters, countOnly)
	if err != nil {
		return nil, err
	}
	out := make([]Bucket, len(buckets))
	for i, b := range buckets {
		out[i] = Bucket{Key: b.Key, Count: b.Count}
	}
	return out, nil
}
