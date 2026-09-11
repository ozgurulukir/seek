package search

import (
	"context"
)

// SearchRepository is the narrow persistence seam consumed by Engine. The
// engine never owns a SQLite handle or constructs SQL; the composition root
// supplies the production adapter and fakes can implement this interface in
// unit tests.
type SearchRepository interface {
	SearchFTS(ctx context.Context, query string, limit int, filters *FilterSet) ([]Result, error)
	SearchVector(ctx context.Context, query []float32, limit int, filters *FilterSet) ([]Result, error)
	// SortValues returns the sort key for each document: a string or float64
	// per ID. Document-column pseudo-fields (created_at, line_count, mtime,
	// path, title) are resolved by the adapter from the documents row; every
	// other name is a fast field. Documents without a value are absent from
	// the map (the engine sorts them last, preserving relative order).
	SortValues(ctx context.Context, documentIDs []int64, field string) (map[int64]interface{}, error)
	GetChunkContent(ctx context.Context, chunkID int64) (string, error)
	ExecuteAggregation(ctx context.Context, aggregation Aggregation, filters *FilterSet) ([]Bucket, error)
}
