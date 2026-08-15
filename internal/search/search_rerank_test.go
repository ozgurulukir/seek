package search_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

type mockReranker struct {
	rerankFn func(ctx context.Context, query string, documents []string, topN int) ([]embed.RerankResult, error)
}

func (m *mockReranker) Rerank(ctx context.Context, query string, documents []string, topN int) ([]embed.RerankResult, error) {
	if m.rerankFn != nil {
		return m.rerankFn(ctx, query, documents, topN)
	}
	return nil, nil
}

func TestSearchWithReranker(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	col, err := db.CreateCollection("code", store.CollectionTypeCode, tmpDir, "**/*")
	if err != nil {
		t.Fatal(err)
	}

	doc1, _ := db.UpsertDocument(col.ID, "/path/doc1.go", "doc1.go", "h1", 1000, 50)
	doc2, _ := db.UpsertDocument(col.ID, "/path/doc2.go", "doc2.go", "h2", 1000, 50)

	_ = db.UpsertFTS(doc1, "doc1.go", "apple banana")
	_ = db.InsertChunkWithLines(doc1, 0, "apple banana", 1, 20, nil)

	_ = db.UpsertFTS(doc2, "doc2.go", "apple orange")
	_ = db.InsertChunkWithLines(doc2, 0, "apple orange", 1, 20, nil)

	engine := search.NewEngine(db, nil)

	// Invert the order with mock reranker
	mockR := &mockReranker{
		rerankFn: func(ctx context.Context, query string, documents []string, topN int) ([]embed.RerankResult, error) {
			return []embed.RerankResult{
				{Index: 1, RelevanceScore: 0.99},
				{Index: 0, RelevanceScore: 0.20},
			}, nil
		},
	}
	engine.WithReranker(mockR)

	results, err := engine.SearchBM25(context.Background(), "apple", 10, search.Options{QueryMode: "raw"})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "doc2.go" {
		t.Errorf("expected doc2.go first after reranking, got %s", results[0].Title)
	}
	if results[0].Score != 0.99 {
		t.Errorf("expected score 0.99, got %f", results[0].Score)
	}
}
