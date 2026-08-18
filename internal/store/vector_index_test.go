package store

import (
	"testing"
)

func TestHNSWIndex_Add(t *testing.T) {
	idx, err := newHNSWIndex(3, 16, 50)
	if err != nil {
		t.Fatalf("failed to create hnsw index: %v", err)
	}

	// Test happy path
	err = idx.Add(1, []float32{0.1, 0.2, 0.3})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if idx.Len() != 1 {
		t.Errorf("expected length 1, got %d", idx.Len())
	}

	if !idx.dirty {
		t.Errorf("expected dirty flag to be true")
	}

	// Test dimension mismatch
	err = idx.Add(2, []float32{0.1, 0.2}) // only 2 dims instead of 3
	if err == nil {
		t.Errorf("expected error for dimension mismatch, got nil")
	}
}

func TestLinearIndex_Add(t *testing.T) {
	idx := newLinearIndex(3)

	// Test happy path
	err := idx.Add(1, []float32{0.1, 0.2, 0.3})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if idx.Len() != 1 {
		t.Errorf("expected length 1, got %d", idx.Len())
	}

	// Test dimension mismatch
	err = idx.Add(2, []float32{0.1, 0.2})
	if err == nil {
		t.Errorf("expected error for dimension mismatch, got nil")
	}
}
