package search

import (
	"context"
	"testing"
)

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
