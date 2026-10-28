package store

import (
	"errors"
	"strings"
	"testing"
)

// stubVectorIndex is a minimal VectorIndex fake whose Search behavior is
// scripted per test.
type stubVectorIndex struct {
	searchResults []VectorResult
	searchErr     error
}

func (s *stubVectorIndex) Add(int64, []float32) error { return nil }
func (s *stubVectorIndex) Delete(int64) error         { return nil }
func (s *stubVectorIndex) Clear() error               { return nil }
func (s *stubVectorIndex) Save(string) error          { return nil }
func (s *stubVectorIndex) Load(string) error          { return nil }
func (s *stubVectorIndex) Len() int                   { return len(s.searchResults) }
func (s *stubVectorIndex) Contains(int64) bool        { return false }
func (s *stubVectorIndex) Search([]float32, int) ([]VectorResult, error) {
	return s.searchResults, s.searchErr
}

// TestSearchVectorHNSWErrorPropagates pins the M1 contract: a failing HNSW
// lookup must surface as an error, never silently degrade to the linear scan.
// The old code fell through on any error, masking broken index state as
// either a performance cliff or quietly wrong results
// (review 2026-09-17 M1).
func TestSearchVectorHNSWErrorPropagates(t *testing.T) {
	s := newTestStore(t)
	boom := errors.New("corrupt graph")
	s.SetVectorIndex(&stubVectorIndex{searchErr: boom})

	_, err := s.SearchVector([]float32{0.1, 0.2, 0.3}, 10, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("expected HNSW error to propagate, got %v", err)
	}
	if !strings.Contains(err.Error(), "hnsw search") {
		t.Fatalf("expected 'hnsw search' context in error, got %q", err.Error())
	}
}

// TestSearchVectorEmptyHNSWFallsBackToLinear pins the complementary contract:
// an empty HNSW result set is a legitimate state (no graph yet) and must
// still fall back to the linear scan, which finds the real embeddings.
func TestSearchVectorEmptyHNSWFallsBackToLinear(t *testing.T) {
	s := newTestStore(t)
	insertVecDoc(t, s, "LinearFindable", [][]float32{{1, 0, 0}})
	s.SetVectorIndex(&stubVectorIndex{})

	results, err := s.SearchVector([]float32{1, 0, 0}, 10, nil)
	if err != nil {
		t.Fatalf("SearchVector: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected linear-scan fallback to find the embedded chunk, got no results")
	}
}

// TestSyncVectorIndexFull_BatchBoundaries verifies the keyset-paginated full
// rebuild processes every embedded chunk across batch boundaries.
func TestSyncVectorIndexFull_BatchBoundaries(t *testing.T) {
	for _, n := range []int{500, 501, 1001} {
		s := newTestStore(t)
		idx := newLinearIndex(4)
		s.SetVectorIndex(idx)

		col, err := s.CreateCollection("vec-full", "markdown", "/tmp", "**/*.md")
		if err != nil {
			t.Fatalf("CreateCollection: %v", err)
		}
		docID, err := s.UpsertDocument(col.ID, "/path/to/doc", "Doc", "hash", 1, 1)
		if err != nil {
			t.Fatalf("UpsertDocument: %v", err)
		}

		emb := []float32{0.1, 0.2, 0.3, 0.4}
		for i := 0; i < n; i++ {
			if err := s.InsertChunk(docID, i, "chunk", emb); err != nil {
				t.Fatalf("InsertChunk %d: %v", i, err)
			}
		}

		added, err := s.SyncVectorIndexIncremental()
		if err != nil {
			t.Fatalf("SyncVectorIndexIncremental (full path): %v", err)
		}
		if added != n {
			t.Fatalf("added = %d, want %d", added, n)
		}
		if idx.Len() != n {
			t.Fatalf("vector index len = %d, want %d", idx.Len(), n)
		}
	}
}
