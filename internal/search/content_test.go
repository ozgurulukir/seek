package search

import (
	"context"
	"errors"
	"testing"
)

type mockContentRepository struct {
	contents map[int64]string
	errs     map[int64]error
}

func (m *mockContentRepository) SearchFTS(ctx context.Context, query string, limit int, filters *FilterSet) ([]Result, error) {
	return nil, nil
}

func (m *mockContentRepository) SearchVector(ctx context.Context, query []float32, limit int, filters *FilterSet) ([]Result, error) {
	return nil, nil
}

func (m *mockContentRepository) SortValues(ctx context.Context, documentIDs []int64, field string) (map[int64]interface{}, error) {
	return nil, nil
}

func (m *mockContentRepository) GetChunkContent(ctx context.Context, chunkID int64) (string, error) {
	if err, ok := m.errs[chunkID]; ok && err != nil {
		return "", err
	}
	if content, ok := m.contents[chunkID]; ok {
		return content, nil
	}
	return "", errors.New("chunk not found")
}

func (m *mockContentRepository) ExecuteAggregation(ctx context.Context, aggregation Aggregation, filters *FilterSet) ([]Bucket, error) {
	return nil, nil
}

func TestEnrichContent_NilOrEmptyResults(t *testing.T) {
	e := &Engine{}
	ctx := context.Background()

	if err := e.EnrichContent(ctx, nil); err != nil {
		t.Fatalf("expected nil error for nil results, got: %v", err)
	}

	var empty []Result
	if err := e.EnrichContent(ctx, empty); err != nil {
		t.Fatalf("expected nil error for empty results, got: %v", err)
	}
}

func TestEnrichContent_ChunkIDZeroOrNegative(t *testing.T) {
	repo := &mockContentRepository{
		contents: map[int64]string{0: "should not be fetched", -1: "should not be fetched"},
	}
	e := NewEngine(repo, nil)
	ctx := context.Background()

	results := []Result{
		{ChunkID: 0, Content: "Hello >>>world<<<!"},
		{ChunkID: -1, Content: ">>>Start<<< and >>>End<<<"},
	}

	if err := e.EnrichContent(ctx, results); err != nil {
		t.Fatalf("EnrichContent failed: %v", err)
	}

	if results[0].Content != "Hello world!" {
		t.Errorf("expected 'Hello world!', got '%s'", results[0].Content)
	}
	if results[1].Content != "Start and End" {
		t.Errorf("expected 'Start and End', got '%s'", results[1].Content)
	}
}

func TestEnrichContent_NilEngineOrNilRepository(t *testing.T) {
	ctx := context.Background()
	results := []Result{
		{ChunkID: 10, Content: "original content"},
	}

	// 1. Nil Engine
	var nilEngine *Engine
	if err := nilEngine.EnrichContent(ctx, results); err != nil {
		t.Fatalf("expected nil error for nil engine, got: %v", err)
	}
	if results[0].Content != "original content" {
		t.Errorf("expected 'original content', got '%s'", results[0].Content)
	}

	// 2. Engine with nil repository
	engineNilRepo := &Engine{repository: nil}
	if err := engineNilRepo.EnrichContent(ctx, results); err != nil {
		t.Fatalf("expected nil error for nil repository, got: %v", err)
	}
	if results[0].Content != "original content" {
		t.Errorf("expected 'original content', got '%s'", results[0].Content)
	}
}

func TestEnrichContent_Success(t *testing.T) {
	repo := &mockContentRepository{
		contents: map[int64]string{
			100: "Content for chunk 100",
			200: "Content for chunk 200",
		},
	}
	e := NewEngine(repo, nil)
	ctx := context.Background()

	results := []Result{
		{ChunkID: 100, Content: "old 100"},
		{ChunkID: 200, Content: "old 200"},
	}

	if err := e.EnrichContent(ctx, results); err != nil {
		t.Fatalf("EnrichContent failed: %v", err)
	}

	if results[0].Content != "Content for chunk 100" {
		t.Errorf("expected 'Content for chunk 100', got '%s'", results[0].Content)
	}
	if results[1].Content != "Content for chunk 200" {
		t.Errorf("expected 'Content for chunk 200', got '%s'", results[1].Content)
	}
}

func TestEnrichContent_RepositoryError(t *testing.T) {
	dbErr := errors.New("database connection failed")
	repo := &mockContentRepository{
		errs: map[int64]error{
			300: dbErr,
		},
	}
	e := NewEngine(repo, nil)
	ctx := context.Background()

	results := []Result{
		{ChunkID: 300, Content: "chunk 300"},
	}

	err := e.EnrichContent(ctx, results)
	if err == nil {
		t.Fatal("expected error from EnrichContent, got nil")
	}

	wantSubstring := "enrich chunk 300: database connection failed"
	if err.Error() != wantSubstring {
		t.Errorf("expected error message '%s', got '%s'", wantSubstring, err.Error())
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("expected errors.Is(err, dbErr) to be true")
	}
}

func TestEnrichContent_MixedResults(t *testing.T) {
	repo := &mockContentRepository{
		contents: map[int64]string{
			1: "Loaded chunk 1 content",
		},
	}
	e := NewEngine(repo, nil)
	ctx := context.Background()

	results := []Result{
		{ChunkID: 0, Content: ">>>Snippet<<< 0"},
		{ChunkID: 1, Content: "To be replaced"},
		{ChunkID: -5, Content: ">>>Snippet<<< -5"},
	}

	if err := e.EnrichContent(ctx, results); err != nil {
		t.Fatalf("EnrichContent failed: %v", err)
	}

	if results[0].Content != "Snippet 0" {
		t.Errorf("expected 'Snippet 0', got '%s'", results[0].Content)
	}
	if results[1].Content != "Loaded chunk 1 content" {
		t.Errorf("expected 'Loaded chunk 1 content', got '%s'", results[1].Content)
	}
	if results[2].Content != "Snippet -5" {
		t.Errorf("expected 'Snippet -5', got '%s'", results[2].Content)
	}
}
