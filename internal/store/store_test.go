package store

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags fts5 ./... or make test")
		}
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

	res, err := s.SearchVector([]float32{1, 0}, 2, nil)
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
	res, err := s.SearchVector([]float32{1, 0}, 5, nil)
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

func TestSearchFTS(t *testing.T) {
	store := newTestStore(t)

	col, err := store.CreateCollection("test-col", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	// Insert test documents
	doc1ID, err := store.UpsertDocument(col.ID, "/path/to/doc1", "Doc 1", "hash1", 1, 1)
	if err != nil {
		t.Fatalf("InsertDocument 1: %v", err)
	}
	if err := store.UpsertFTS(doc1ID, "Security Vulnerability Fix", "Content describing an SQL injection issue in search logic."); err != nil {
		t.Fatalf("UpsertFTS 1: %v", err)
	}

	doc2ID, err := store.UpsertDocument(col.ID, "/path/to/doc2", "Doc 2", "hash2", 1, 1)
	if err != nil {
		t.Fatalf("InsertDocument 2: %v", err)
	}
	if err := store.UpsertFTS(doc2ID, "Performance Update", "Optimizing the indexer performance and chunking speed."); err != nil {
		t.Fatalf("UpsertFTS 2: %v", err)
	}

	// Test regular SearchFTS execution
	results, err := store.SearchFTS("injection", 10, nil)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].DocumentID != doc1ID {
		t.Errorf("expected to find doc1 (id %d), got id %d", doc1ID, results[0].DocumentID)
	}
}

func TestStore_AutocompleteTerms(t *testing.T) {
	store := newTestStore(t)

	col, err := store.CreateCollection("test-col", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	docID, err := store.UpsertDocument(col.ID, "/path/to/doc", "Doc", "hash", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if err := store.UpsertFTS(docID, "Algorithm Testing", "Architecture indexing autocomplete performance"); err != nil {
		t.Fatalf("UpsertFTS: %v", err)
	}

	terms, err := store.AutocompleteTerms("algo", 5)
	if err != nil {
		t.Fatalf("AutocompleteTerms failed: %v", err)
	}
	if len(terms) == 0 || terms[0] != "algorithm" {
		t.Errorf("expected ['algorithm'], got %v", terms)
	}

	termsAuto, err := store.AutocompleteTerms("auto", 5)
	if err != nil {
		t.Fatalf("AutocompleteTerms failed: %v", err)
	}
	if len(termsAuto) == 0 || termsAuto[0] != "autocomplete" {
		t.Errorf("expected ['autocomplete'], got %v", termsAuto)
	}
}

func TestSyncVectorIndexIncremental(t *testing.T) {
	store := newTestStore(t)
	vi := newLinearIndex(2)
	store.SetVectorIndex(vi)

	col, err := store.CreateCollection("vec-col", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	docID, err := store.UpsertDocument(col.ID, "/path/to/doc1", "Doc 1", "hash1", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}

	// Insert chunk 1 with embedding
	if err := store.InsertChunk(docID, 0, "chunk 1", []float32{1.0, 0.0}); err != nil {
		t.Fatalf("InsertChunk 1: %v", err)
	}

	// 1. Initial sync (from empty)
	added, err := store.SyncVectorIndexIncremental()
	if err != nil {
		t.Fatalf("initial SyncVectorIndexIncremental: %v", err)
	}
	if vi.Len() != 1 {
		t.Errorf("expected 1 vector in index, got %d", vi.Len())
	}

	// 2. Second sync without new chunks (should add 0)
	added, err = store.SyncVectorIndexIncremental()
	if err != nil {
		t.Fatalf("second SyncVectorIndexIncremental: %v", err)
	}
	if added != 0 {
		t.Errorf("expected 0 new chunks added, got %d", added)
	}
	if vi.Len() != 1 {
		t.Errorf("expected index length 1, got %d", vi.Len())
	}

	// 3. Add second chunk
	if err := store.InsertChunk(docID, 1, "chunk 2", []float32{0.0, 1.0}); err != nil {
		t.Fatalf("InsertChunk 2: %v", err)
	}

	added, err = store.SyncVectorIndexIncremental()
	if err != nil {
		t.Fatalf("third SyncVectorIndexIncremental: %v", err)
	}
	if added != 1 {
		t.Errorf("expected 1 new chunk added, got %d", added)
	}
	if vi.Len() != 2 {
		t.Errorf("expected index length 2, got %d", vi.Len())
	}

	// 4. Delete document (and its chunks) -> verify ghost cleanup on next sync
	if err := store.DeleteDocument(docID); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	// At this point vi still has 2 vectors, but DB has 0 embedded chunks.
	// Next incremental sync should detect vi.Len() > dbCount and purge stale vectors.
	_, err = store.SyncVectorIndexIncremental()
	if err != nil {
		t.Fatalf("SyncVectorIndexIncremental after delete: %v", err)
	}
	if vi.Len() != 0 {
		t.Errorf("expected index length 0 after ghost purge, got %d", vi.Len())
	}
}

func TestUpdateChunkEmbeddingUpdatesVectorIndex(t *testing.T) {
	store := newTestStore(t)
	vi := newLinearIndex(2)
	store.SetVectorIndex(vi)

	col, err := store.CreateCollection("vec-col", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	docID, err := store.UpsertDocument(col.ID, "/path/to/doc", "Doc", "hash", 1, 1)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if err := store.InsertChunk(docID, 0, "chunk", []float32{1, 0}); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}
	if _, err := store.SyncVectorIndexIncremental(); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	if err := store.UpdateChunkEmbedding(1, []float32{0, 1}); err != nil {
		t.Fatalf("UpdateChunkEmbedding: %v", err)
	}
	results, err := vi.Search([]float32{0, 1}, 1)
	if err != nil {
		t.Fatalf("vector search: %v", err)
	}
	if len(results) != 1 || results[0].ChunkID != 1 || results[0].Score < 0.99 {
		t.Fatalf("expected updated vector result, got %v", results)
	}
}

func TestFTSRebuildOnTokenizerChange(t *testing.T) {
	store := newTestStore(t)

	col, err := store.CreateCollection("fts-rebuild-col", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	docID, err := store.UpsertDocument(col.ID, "/path/doc.md", "Tokenizer Document", "hash123", 1.0, 5)
	if err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}

	if err := store.InsertChunk(docID, 0, "rebuilt full text search vocabulary", nil); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}
	if err := store.UpsertFTS(docID, "Tokenizer Document", "rebuilt full text search vocabulary"); err != nil {
		t.Fatalf("UpsertFTS: %v", err)
	}

	// Simulate an older DB version with a different tokenizer (e.g. "unicode61" without diacritics config)
	_, err = store.db.Exec(`DROP TABLE IF EXISTS documents_fts_vocab`)
	if err != nil {
		t.Fatalf("drop vocab: %v", err)
	}
	_, err = store.db.Exec(`DROP TABLE IF EXISTS documents_fts`)
	if err != nil {
		t.Fatalf("drop fts: %v", err)
	}
	oldDDL := `CREATE VIRTUAL TABLE documents_fts USING fts5(title, content, content_rowid='id', tokenize='unicode61')`
	if _, err := store.db.Exec(oldDDL); err != nil {
		t.Fatalf("create old fts: %v", err)
	}

	// Verify ftsNeedsRebuild detects the outdated tokenizer
	needsRebuild, err := store.ftsNeedsRebuild()
	if err != nil {
		t.Fatalf("ftsNeedsRebuild: %v", err)
	}
	if !needsRebuild {
		t.Fatal("expected ftsNeedsRebuild to be true for outdated tokenizer")
	}

	// Trigger migration/initFTS
	if err := store.initFTS(); err != nil {
		t.Fatalf("initFTS migration failed: %v", err)
	}

	// Verify ftsNeedsRebuild is now false
	needsRebuild, err = store.ftsNeedsRebuild()
	if err != nil {
		t.Fatalf("ftsNeedsRebuild after migration: %v", err)
	}
	if needsRebuild {
		t.Fatal("expected ftsNeedsRebuild to be false after migration")
	}

	// Verify SearchFTS works with rebuilt index
	searchResults, err := store.SearchFTS("vocabulary", 5, nil)
	if err != nil {
		t.Fatalf("SearchFTS after rebuild: %v", err)
	}
	if len(searchResults) == 0 {
		t.Fatal("expected search results after rebuild, got none")
	}

	// Verify AutocompleteTerms works with recreated vocab table
	terms, err := store.AutocompleteTerms("vocab", 5)
	if err != nil {
		t.Fatalf("AutocompleteTerms after rebuild: %v", err)
	}
	if len(terms) == 0 || terms[0] != "vocabulary" {
		t.Fatalf("expected autocomplete ['vocabulary'], got %v", terms)
	}
}
