package store

import (
	"testing"
)

func TestHNSWIndex_Search(t *testing.T) {
	dim := 3
	idx, err := newHNSWIndex(dim, 16, 50)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	// Test Search on empty index
	query := []float32{1.0, 0.0, 0.0}
	results, err := idx.Search(query, 2)
	if err != nil {
		t.Fatalf("unexpected error searching empty index: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}

	// Add vectors
	err = idx.Add(1, []float32{1.0, 0.0, 0.0})
	if err != nil {
		t.Fatalf("failed to add vector 1: %v", err)
	}
	err = idx.Add(2, []float32{0.0, 1.0, 0.0})
	if err != nil {
		t.Fatalf("failed to add vector 2: %v", err)
	}
	err = idx.Add(3, []float32{0.0, 0.0, 1.0})
	if err != nil {
		t.Fatalf("failed to add vector 3: %v", err)
	}

	// Test Search
	results, err = idx.Search(query, 2)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ChunkID != 1 {
		t.Errorf("expected best match to be chunk 1, got %d", results[0].ChunkID)
	}
	if results[0].Score <= 0.99 {
		t.Errorf("expected score close to 1.0 for exact match, got %f", results[0].Score)
	}

	// Test dimension mismatch
	_, err = idx.Search([]float32{1.0, 0.0}, 2)
	if err == nil {
		t.Error("expected error for dimension mismatch, got nil")
	}

	// k > len
	results, err = idx.Search([]float32{1.0, 0.0, 0.0}, 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	// We added 3 vectors, so even if we ask for 10, we should get at most 3
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}
