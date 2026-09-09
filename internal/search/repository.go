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
	BatchGetFastFields(ctx context.Context, documentIDs []int64, field string) (map[int64]interface{}, error)
	GetChunkContent(ctx context.Context, chunkID int64) (string, error)
	ExecuteAggregation(ctx context.Context, aggregation Aggregation, filters *FilterSet) ([]Bucket, error)
}
