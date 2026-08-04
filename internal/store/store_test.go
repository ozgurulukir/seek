package store

import (
	"path/filepath"
	"sort"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func insertVecDoc(t *testing.T, s *Store, title string, embs [][]float32) {
	t.Helper()
	col, err := s.CreateCollection("col-"+title, "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	docID, err := s.UpsertDocument(col.ID, "/tmp/"+title+".md", title, "hash", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	for i, e := range embs {
		if err := s.InsertChunk(docID, i, title+" chunk", e); err != nil {
			t.Fatalf("InsertChunk: %v", err)
		}
	}
}

// cosineSimilarity edge cases must keep returning 0 (not panic / NaN),
// matching prior hand-rolled behavior after switching to vek32.
func TestCosineSimilarityEdgeCases(t *testing.T) {
	if got := cosineSimilarity(nil, nil); got != 0 {
		t.Errorf("nil/nil = %v, want 0", got)
	}
	if got := cosineSimilarity([]float32{1, 2}, []float32{1}); got != 0 {
		t.Errorf("mismatch len = %v, want 0", got)
	}
	if got := cosineSimilarity([]float32{0, 0}, []float32{1, 1}); got != 0 {
		t.Errorf("zero a = %v, want 0", got)
	}
	if got := cosineSimilarity([]float32{1, 1}, []float32{0, 0}); got != 0 {
		t.Errorf("zero b = %v, want 0", got)
	}
	closeEnough := func(a, b float64) bool {
		d := a - b
		if d < 0 {
			d = -d
		}
		return d < 1e-6
	}
	if got := cosineSimilarity([]float32{1, 0}, []float32{1, 0}); !closeEnough(got, 1) {
		t.Errorf("identical = %v, want ~1", got)
	}
	if got := cosineSimilarity([]float32{1, 0}, []float32{0, 1}); !closeEnough(got, 0) {
		t.Errorf("orthogonal = %v, want ~0", got)
	}
}

// SearchVector must return nearest neighbours sorted by similarity, and cap at limit.
func TestSearchVectorOrdersBySimilarity(t *testing.T) {
	s := newTestStore(t)
	// query = [1, 0]; nearest first, farthest last.
	insertVecDoc(t, s, "near", [][]float32{{0.99, 0.1}})
	insertVecDoc(t, s, "far", [][]float32{{0.1, 0.99}})
	insertVecDoc(t, s, "mid", [][]float32{{0.6, 0.8}})

	res, err := s.SearchVector([]float32{1, 0}, 2)
	if err != nil {
		t.Fatalf("SearchVector: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("len = %d, want 2 (limit)", len(res))
	}
	if res[0].Title != "near" || res[1].Title != "mid" {
		t.Errorf("got order [%s, %s], want [near, mid]", res[0].Title, res[1].Title)
	}
	// Scores must be descending and in (0,1].
	if res[0].Score < res[1].Score {
		t.Errorf("scores not descending: %v > %v", res[0].Score, res[1].Score)
	}
}

// SearchVector returns nothing for an empty embedding table without error.
func TestSearchVectorEmpty(t *testing.T) {
	s := newTestStore(t)
	res, err := s.SearchVector([]float32{1, 0}, 5)
	if err != nil {
		t.Fatalf("SearchVector on empty: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("len = %d, want 0", len(res))
	}
}

var sink float64

func BenchmarkCosineSimilarity(b *testing.B) {
	a := make([]float32, 1024)
	c := make([]float32, 1024)
	for i := range a {
		a[i] = float32(i%7) / 10
		c[i] = float32(i%5) / 10
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = cosineSimilarity(a, c)
	}
	_ = sink
}

func BenchmarkBubbleSort(b *testing.B) {
	// Reconstruct the old O(n^2) bubble sort for a size-N slice of scores.
	mk := func(n int) []struct{ score float64 } {
		s := make([]struct{ score float64 }, n)
		for i := range s {
			s[i].score = float64((i * 7919) % n)
		}
		return s
	}
	b.Run("bubble_2000", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			all := mk(2000)
			for x := 0; x < len(all); x++ {
				for j := x + 1; j < len(all); j++ {
					if all[j].score > all[x].score {
						all[x], all[j] = all[j], all[x]
					}
				}
			}
		}
	})
	b.Run("sortSlice_2000", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			all := mk(2000)
			sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })
		}
	})
}
