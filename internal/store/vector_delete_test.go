package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// The T6 contract (audit 2026-11-21), pinned per backend:
//
//   - linearIndex supports a safe per-id Delete: committed SQLite chunk
//     deletions purge matching graph entries in the same transaction, so a
//     deleted chunk can neither be returned by vector search nor displace
//     live results until the next sync.
//   - hnswIndex must NOT use coder/hnsw v0.6.1 Graph.Delete: it leaks the
//     deleted node back into Search results and panics with a nil
//     dereference nondeterministically (map iteration order; probe
//     2026-11-21). hnswIndex.Delete therefore refuses with
//     ErrVectorDeleteUnsupported, and stale HNSW entries are purged by the
//     Len()>dbCount full-rebuild heuristic in
//     SyncVectorIndexIncrementalContext.
//
// If a future coder/hnsw release ships a safe Delete, flip the
// ErrVectorDeleteUnsupported refusal back to a real graph mutation and add a
// tiny-graph regression test here (Delete, then Search, no panic).

func TestVectorIndexDeleteContract(t *testing.T) {
	t.Run("hnsw refuses unsafe delete", func(t *testing.T) {
		idx, err := newHNSWIndex(2, 16, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []int64{1, 2} {
			if err := idx.Add(id, []float32{float32(id) / 2, 0.5}); err != nil {
				t.Fatalf("Add %d: %v", id, err)
			}
		}
		if err := idx.Delete(1); !errors.Is(err, ErrVectorDeleteUnsupported) {
			t.Fatalf("Delete err = %v, want ErrVectorDeleteUnsupported", err)
		}
		// The graph must be untouched: no silent corruption.
		if !idx.Contains(1) || idx.Len() != 2 {
			t.Errorf("graph mutated by refused Delete: contains1=%v len=%d", idx.Contains(1), idx.Len())
		}
		// Search stays healthy after the refusal.
		if _, err := idx.Search([]float32{0.5, 0.5}, 5); err != nil {
			t.Errorf("Search after refused Delete: %v", err)
		}
	})

	t.Run("linear deletes safely", func(t *testing.T) {
		idx := newLinearIndex(2)
		for _, v := range []struct {
			id  int64
			vec []float32
		}{{1, []float32{1, 0}}, {2, []float32{0, 1}}} {
			if err := idx.Add(v.id, v.vec); err != nil {
				t.Fatalf("Add %d: %v", v.id, err)
			}
		}
		if err := idx.Delete(1); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if idx.Contains(1) || idx.Len() != 1 {
			t.Errorf("after Delete: Contains(1)=%v Len=%d, want false/1", idx.Contains(1), idx.Len())
		}
		results, err := idx.Search([]float32{1, 0}, 5)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(results) != 1 || results[0].ChunkID != 2 {
			t.Errorf("expected only surviving id 2, got %v", results)
		}
		// Deleting a missing id is a no-op, not an error.
		if err := idx.Delete(999); err != nil {
			t.Errorf("Delete of missing id: %v", err)
		}
	})
}

// newVectorDeleteTestStore inserts one markdown collection with two documents,
// each carrying one embedded chunk, and populates the vector index. The
// collection ID is 1 (fresh DB) and doc2's chunk is doc1's chunk ID + 1.
func newVectorDeleteTestStore(t *testing.T, vi VectorIndex) (*Store, int64, int64) {
	t.Helper()
	s := newTestStore(t)
	if vi != nil {
		s.SetVectorIndex(vi)
	}
	col, err := s.CreateCollection("vec-col", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	doc1, err := s.UpsertDocument(col.ID, "/tmp/doc1", "Doc 1", "hash1", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument doc1: %v", err)
	}
	doc2, err := s.UpsertDocument(col.ID, "/tmp/doc2", "Doc 2", "hash2", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument doc2: %v", err)
	}
	for i, e := range []struct {
		doc int64
		vec []float32
	}{{doc1, []float32{1, 0}}, {doc2, []float32{0, 1}}} {
		if err := s.InsertChunk(e.doc, i, fmt.Sprintf("chunk %d", i), e.vec); err != nil {
			t.Fatalf("InsertChunk %d: %v", i, err)
		}
	}
	if _, err := s.SyncVectorIndexIncrementalContext(context.Background()); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	return s, doc1, doc2
}

func TestDeleteDocumentPurgesVectorEntries(t *testing.T) {
	for _, backend := range []struct {
		name string
		mk   func() (VectorIndex, error)
	}{
		{"hnsw", func() (VectorIndex, error) { return newHNSWIndex(2, 16, 50) }},
		{"linear", func() (VectorIndex, error) { return newLinearIndex(2), nil }},
	} {
		t.Run(backend.name, func(t *testing.T) {
			idx, err := backend.mk()
			if err != nil {
				t.Fatal(err)
			}
			s, doc1, _ := newVectorDeleteTestStore(t, idx)
			if idx.Len() != 2 {
				t.Fatalf("pre-delete Len = %d, want 2", idx.Len())
			}

			if err := s.DeleteDocumentContext(context.Background(), doc1); err != nil {
				t.Fatalf("DeleteDocumentContext: %v", err)
			}

			// The next incremental sync must converge without an embedding
			// pass: for linear immediately (purge in the delete tx), for hnsw
			// via the Len()>dbCount full-rebuild heuristic (which reports the
			// rebuilt entries as added).
			added, err := s.SyncVectorIndexIncrementalContext(context.Background())
			if err != nil {
				t.Fatalf("post-delete incremental sync: %v", err)
			}
			if idx.Len() != 1 {
				t.Errorf("post-delete sync len=%d, want 1", idx.Len())
			}
			if added < 0 || (backend.name == "linear" && added != 0) {
				t.Errorf("post-delete sync added=%d (linear wants 0)", added)
			}
			// The survivor (doc2, vector {0,1}) is still searchable and no
			// longer displaced by the deleted entry.
			results, err := idx.Search([]float32{0, 1}, 2)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(results) == 0 || results[0].ChunkID != doc1+1 {
				t.Errorf("expected doc2 chunk as best match, got %v", results)
			}
		})
	}
}

func TestUpsertAndReplacePurgesReplacedChunkVectors(t *testing.T) {
	idx := newLinearIndex(2)
	s, doc1, _ := newVectorDeleteTestStore(t, idx)

	// Replace doc1 with new chunks that have no embeddings yet: the old chunk's
	// graph entry must go immediately instead of waiting for the heuristic.
	replaced, err := s.UpsertAndReplaceIndex(context.Background(), DocumentIndex{
		CollectionID: 1,
		Path:         "/tmp/doc1",
		Title:        "Doc 1",
		ContentHash:  "hash1-v2",
		Mtime:        1,
		LineCount:    1,
		FTSContent:   "Doc 1 chunk",
		Chunks:       []IndexChunk{{Seq: 0, Content: "Doc 1 chunk v2", StartLine: 1, EndLine: 1}},
	})
	if err != nil {
		t.Fatalf("UpsertAndReplaceIndex: %v", err)
	}
	if replaced != doc1 {
		t.Fatalf("replaced doc id = %d, want %d", replaced, doc1)
	}
	if idx.Len() != 1 {
		t.Errorf("post-replace Len = %d, want 1 (only doc2 entry remains)", idx.Len())
	}
}

func TestDeleteOrphansPurgesVectorEntries(t *testing.T) {
	idx := newLinearIndex(2)
	s, _, _ := newVectorDeleteTestStore(t, idx)

	// Full-collection orphan purge (reindex): both documents go stale.
	removed, err := s.DeleteOrphansContext(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("DeleteOrphansContext: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if idx.Len() != 0 {
		t.Errorf("post-purge Len = %d, want 0", idx.Len())
	}
}

func TestDeleteCollectionPurgesVectorEntries(t *testing.T) {
	idx := newLinearIndex(2)
	s, _, _ := newVectorDeleteTestStore(t, idx)

	if err := s.DeleteCollection(1); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	if idx.Len() != 0 {
		t.Errorf("post-delete Len = %d, want 0", idx.Len())
	}
}
