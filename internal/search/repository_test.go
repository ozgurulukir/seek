package search

import (
	"context"
	"errors"
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
	target := &testStoreFilterTarget{filters: store.NewFilterSet()}
	if err := filters.Apply(target); err != nil {
		return nil, err
	}
	return target.filters, nil
}

type testStoreFilterTarget struct{ filters *store.FilterSet }

func (t *testStoreFilterTarget) AddCollection(name string) {
	t.filters.Add(&store.CollectionFilter{Name: name})
}
func (t *testStoreFilterTarget) AddDocType(typ string) {
	t.filters.Add(&store.DocTypeFilter{Type: typ})
}
func (t *testStoreFilterTarget) AddLanguage(language string) {
	t.filters.Add(&store.FastFieldFilter{Field: "lang", Value: language})
}
func (t *testStoreFilterTarget) AddTag(tag string) { t.filters.Add(&store.TagFilter{Tag: tag}) }
func (t *testStoreFilterTarget) AddRepository(repository string) {
	t.filters.Add(&store.FastFieldFilter{Field: "repo", Value: repository})
}
func (t *testStoreFilterTarget) AddDateRange(after, before string) {
	t.filters.Add(&store.DateRangeFilter{After: after, Before: before})
}
func (t *testStoreFilterTarget) AddChunkType(chunkType ChunkType) {
	t.filters.Add(&store.ChunkTypeFilter{Type: int(chunkType)})
}
func (t *testStoreFilterTarget) AddPath(pattern string) {
	t.filters.Add(&store.PathFilter{Pattern: pattern})
}
func (t *testStoreFilterTarget) AddWorkspace(workspace string) {
	t.filters.Add(&store.FastFieldFilter{Field: "workspace", Value: workspace})
}
func (t *testStoreFilterTarget) AddFastField(field, value string) {
	t.filters.Add(&store.FastFieldFilter{Field: field, Value: value})
}

type fakeSearchRepository struct {
	filters *FilterSet
	sortErr error
}

func (f *fakeSearchRepository) SearchFTS(ctx context.Context, _ string, _ int, _ *FilterSet) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []Result{{DocumentID: 1, Title: "result"}}, nil
}

func (f *fakeSearchRepository) SearchVector(context.Context, []float32, int, *FilterSet) ([]Result, error) {
	return nil, nil
}

func (f *fakeSearchRepository) BatchGetFastFields(context.Context, []int64, string) (map[int64]interface{}, error) {
	return nil, f.sortErr
}

func TestEnginePropagatesFastFieldSortErrors(t *testing.T) {
	sentinel := errors.New("fast field unavailable")
	repository := &fakeSearchRepository{sortErr: sentinel}
	engine := NewEngine(repository, nil)

	_, err := engine.SearchBM25(context.Background(), "query", 10, Options{SortBy: "mtime"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("sort error = %v, want %v", err, sentinel)
	}
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
