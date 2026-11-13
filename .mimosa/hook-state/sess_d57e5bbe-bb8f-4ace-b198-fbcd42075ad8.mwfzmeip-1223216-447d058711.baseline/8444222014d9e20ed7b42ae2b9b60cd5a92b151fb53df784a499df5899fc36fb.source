package store

import (
	"testing"
)

// TestSearchFTSRanksTitleAboveContent pins the bm25 column-weight contract:
// FTSTitleWeight (10.0) is passed POSITIONALLY into bm25(documents_fts, ?, ?)
// in searchContext, coupled to the DDL column order (title, content). If a
// column is added or reordered in the documents_fts DDL without updating the
// weight arguments, title matches silently stop outranking content matches —
// this test is the automated guard for that invariant.
func TestSearchFTSRanksTitleAboveContent(t *testing.T) {
	s := newTestStore(t)
	col, err := s.CreateCollection("notes", CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	// Doc A matches "quartz" only in its title; doc B only in its body.
	titleDoc, err := s.UpsertDocument(col.ID, "/tmp/title-hit.md", "quartz formation", "h1", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument title-hit: %v", err)
	}
	if err := s.UpsertFTS(titleDoc, "quartz formation", "plain body text about rocks"); err != nil {
		t.Fatalf("UpsertFTS title-hit: %v", err)
	}
	contentDoc, err := s.UpsertDocument(col.ID, "/tmp/content-hit.md", "geology overview", "h2", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument content-hit: %v", err)
	}
	if err := s.UpsertFTS(contentDoc, "geology overview", "deep dive into quartz crystals"); err != nil {
		t.Fatalf("UpsertFTS content-hit: %v", err)
	}

	results, err := s.SearchFTS("quartz", 10, nil)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].DocumentID != titleDoc || results[1].DocumentID != contentDoc {
		t.Fatalf("ranking = [%d %d], want title match %d before content match %d (scores %v/%v)",
			results[0].DocumentID, results[1].DocumentID, titleDoc, contentDoc,
			results[0].Score, results[1].Score)
	}
	if results[0].Score >= results[1].Score {
		t.Fatalf("title match score %v must beat content match score %v", results[0].Score, results[1].Score)
	}
}
