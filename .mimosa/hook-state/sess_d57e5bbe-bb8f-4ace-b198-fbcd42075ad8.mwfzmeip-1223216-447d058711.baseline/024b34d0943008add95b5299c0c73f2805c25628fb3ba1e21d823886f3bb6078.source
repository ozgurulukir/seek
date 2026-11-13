package store

import (
	"context"
	"fmt"
	"testing"
)

func docIndexFor(col *Collection, path, content string) DocumentIndex {
	return DocumentIndex{
		CollectionID: col.ID,
		Path:         path,
		Title:        path,
		ContentHash:  path,
		Mtime:        1,
		LineCount:    1,
		FTSContent:   content,
		Chunks:       []IndexChunk{{Seq: 0, Content: content, StartLine: 1, EndLine: 1}},
	}
}

func TestUpsertAndReplaceIndexBatchContextMultiBatch(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("batch", CollectionTypeMarkdown, t.TempDir(), "*.md")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	const docs = 7
	reqs := make([]DocumentIndex, 0, docs)
	for i := range docs {
		reqs = append(reqs, docIndexFor(col, fmt.Sprintf("doc%d.md", i), fmt.Sprintf("batchdoc number %d", i)))
	}
	// BatchSize=3 forces commit boundaries at 3, 6, 7.
	if err := s.UpsertAndReplaceIndexBatchContext(ctx, reqs, 3); err != nil {
		t.Fatalf("batch write: %v", err)
	}
	for i := range docs {
		count, err := s.CountFTSMatching(ctx, fmt.Sprintf(`"number %d"`, i), nil)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("doc %d missing after multi-batch write (count=%d)", i, count)
		}
	}
}

func TestUpsertAndReplaceIndexBatchContextAbortsOnFirstFailure(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("batch-abort", CollectionTypeMarkdown, t.TempDir(), "*.md")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Three batches of 2: the third document (index 2, first of batch 2) is
	// poisoned with an invalid chunk type. Batches 1 (and 2 up to the poison
	// doc) must roll back, batch 0 must stay committed.
	reqs := make([]DocumentIndex, 0, 6)
	for i := range 6 {
		req := docIndexFor(col, fmt.Sprintf("doc%d.md", i), fmt.Sprintf("abortdoc number %d", i))
		if i == 2 {
			req.Chunks = append(req.Chunks, IndexChunk{Seq: 1, Content: "poison", ChunkType: ChunkType(99)})
		}
		reqs = append(reqs, req)
	}
	if err := s.UpsertAndReplaceIndexBatchContext(ctx, reqs, 2); err == nil {
		t.Fatal("expected poisoned batch to fail")
	}

	// Batch 0 (docs 0 and 1) committed before the poisoned batch and stays.
	for i := range 2 {
		count, err := s.CountFTSMatching(ctx, fmt.Sprintf(`"abortdoc number %d"`, i), nil)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("doc %d from committed batch 0 should stay visible (count=%d)", i, count)
		}
	}
	// Docs 2-5 must not be visible: doc 2 is poisoned, so its batch (docs 2-3)
	// rolls back and docs 4-5 are aborted before their batch runs.
	for i := 2; i < 6; i++ {
		count, err := s.CountFTSMatching(ctx, fmt.Sprintf(`"abortdoc number %d"`, i), nil)
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("doc %d visible after aborted batch (count=%d)", i, count)
		}
	}
}

func TestCountFTSMatchingWithFilters(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	colA, err := s.CreateCollection("count-a", CollectionTypeMarkdown, dir, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	colB, err := s.CreateCollection("count-b", CollectionTypeMarkdown, dir, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, col := range []*Collection{colA, colB} {
		if _, err := s.UpsertAndReplaceIndex(ctx, docIndexFor(col, "shared.md", "sharedword alpha beta")); err != nil {
			t.Fatal(err)
		}
	}

	count, err := s.CountFTSMatching(ctx, `"sharedword"`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("unfiltered count = %d, want 2", count)
	}

	filters := NewFilterSet()
	filters.Add(&CollectionFilter{Name: "count-a"})
	count, err = s.CountFTSMatching(ctx, `"sharedword"`, filters)
	if err != nil {
		t.Fatalf("filtered count: %v", err)
	}
	if count != 1 {
		t.Fatalf("collection-filtered count = %d, want 1", count)
	}

	filters = NewFilterSet()
	filters.Add(&CollectionFilter{Name: "no-such-collection"})
	count, err = s.CountFTSMatching(ctx, `"sharedword"`, filters)
	if err != nil {
		t.Fatalf("non-matching filter count: %v", err)
	}
	if count != 0 {
		t.Fatalf("non-matching filtered count = %d, want 0", count)
	}
}
