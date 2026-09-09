package search

import (
	"context"
	"fmt"
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
)

// NewStoreRepository is test-only convenience for search package tests. The
// production adapter lives in internal/app, keeping search itself free of
// Store imports.
func NewStoreRepository(s *store.Store) SearchRepository {
	return &testStoreRepository{store: s}
}

type testStoreRepository struct{ store *store.Store }

func (r *testStoreRepository) SearchFTS(ctx context.Context, query string, limit int, filters *FilterSet) ([]Result, error) {
	storeFilters, err := testStoreFilters(filters)
	if err != nil {
		return nil, err
	}
	results, err := r.store.SearchFTSContext(ctx, query, limit, storeFilters)
	return testStoreResults(results), err
}

func (r *testStoreRepository) SearchVector(ctx context.Context, query []float32, limit int, filters *FilterSet) ([]Result, error) {
	storeFilters, err := testStoreFilters(filters)
	if err != nil {
		return nil, err
	}
	results, err := r.store.SearchVectorContext(ctx, query, limit, storeFilters)
	return testStoreResults(results), err
}

func (r *testStoreRepository) BatchGetFastFields(ctx context.Context, documentIDs []int64, field string) (map[int64]interface{}, error) {
	return r.store.FastFields().BatchGetContext(ctx, documentIDs, field)
}

func (r *testStoreRepository) GetChunkContent(ctx context.Context, chunkID int64) (string, error) {
	return r.store.GetChunkContentContext(ctx, chunkID)
}

func (r *testStoreRepository) ExecuteAggregation(ctx context.Context, aggregation Aggregation, filters *FilterSet) ([]Bucket, error) {
	storeFilters, err := testStoreFilters(filters)
	if err != nil {
		return nil, err
	}
	spec := aggregation.Spec()
	buckets, err := r.store.ExecuteAggregationContext(ctx, store.AggregationSpec{
		Type: string(spec.Type), Field: spec.Field, Interval: spec.Interval, Ranges: spec.Ranges,
	}, storeFilters)
	if err != nil {
		return nil, err
	}
	out := make([]Bucket, len(buckets))
	for i, bucket := range buckets {
		out[i] = Bucket{Key: bucket.Key, Count: bucket.Count}
	}
	return out, nil
}

func testStoreResults(results []store.SearchResult) []Result {
	out := make([]Result, 0, len(results))
	for _, result := range results {
		out = append(out, Result{DocumentID: result.DocumentID, ChunkID: result.ChunkID, Seq: result.Seq, Title: result.Title, Path: result.Path, Collection: result.Collection, Content: result.Content, Score: result.Score, ChunkType: ChunkType(result.ChunkType), ImagePath: result.ImagePath, StartLine: result.StartLine, EndLine: result.EndLine})
	}
	return out
}

func testStoreFilters(filters *FilterSet) (*store.FilterSet, error) {
	if filters == nil || len(filters.Items()) == 0 {
		return nil, nil
	}
	out := store.NewFilterSet()
	for _, filter := range filters.Items() {
		switch filter.Kind {
		case FilterCollection:
			out.Add(&store.CollectionFilter{Name: filter.Value})
		case FilterDocType:
			out.Add(&store.DocTypeFilter{Type: filter.Value})
		case FilterLanguage:
			out.Add(&store.FastFieldFilter{Field: "lang", Value: filter.Value})
		case FilterTag:
			out.Add(&store.TagFilter{Tag: filter.Value})
		case FilterRepository:
			out.Add(&store.FastFieldFilter{Field: "repo", Value: filter.Value})
		case FilterDateRange:
			out.Add(&store.DateRangeFilter{After: filter.After, Before: filter.Before})
		case FilterChunkType:
			out.Add(&store.ChunkTypeFilter{Type: int(filter.Chunk)})
		case FilterPath:
			out.Add(&store.PathFilter{Pattern: filter.Pattern})
		case FilterWorkspace:
			out.Add(&store.FastFieldFilter{Field: "workspace", Value: filter.Value})
		default:
			return nil, fmt.Errorf("unsupported search filter kind %q", filter.Kind)
		}
	}
	return out, nil
}

type fakeSearchRepository struct {
	filters *FilterSet
}

func (f *fakeSearchRepository) SearchFTS(ctx context.Context, _ string, _ int, _ *FilterSet) ([]Result, error) {
	return nil, ctx.Err()
}

func (f *fakeSearchRepository) SearchVector(context.Context, []float32, int, *FilterSet) ([]Result, error) {
	return nil, nil
}

func (f *fakeSearchRepository) BatchGetFastFields(context.Context, []int64, string) (map[int64]interface{}, error) {
	return nil, nil
}

func (f *fakeSearchRepository) GetChunkContent(context.Context, int64) (string, error) {
	return "", nil
}

func (f *fakeSearchRepository) ExecuteAggregation(_ context.Context, _ Aggregation, filters *FilterSet) ([]Bucket, error) {
	f.filters = filters
	return []Bucket{{Key: "markdown", Count: 1}}, nil
}

func TestEngineRepositoryPropagatesContextAndAggregationFilters(t *testing.T) {
	repository := &fakeSearchRepository{}
	engine := NewEngine(repository, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.SearchBM25(ctx, "query", 10, Options{}); err == nil {
		t.Fatal("cancelled repository search returned nil error")
	}

	filters := NewFilterSet()
	if _, err := engine.RunAggregations(context.Background(), []string{"type:terms"}, filters); err != nil {
		t.Fatalf("RunAggregations: %v", err)
	}
	if repository.filters != filters {
		t.Fatal("aggregation filters were not passed to repository")
	}
}
