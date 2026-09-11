package search

// The wire contract: the stable JSON shapes consumed by agents and scripts
// through `seek search --json` and the MCP seek_search tool. This module is
// the single source of truth for those shapes — the CLI and MCP surfaces map
// through NewSearchResults/NewSearchOutput instead of keeping parallel
// structs, so a field addition updates both surfaces in one place.

// SearchResult is the stable JSON wire form of one ranked hit. content_kind
// is derived at mapping time via ContentKind ("full" for chunk-level hits
// carrying complete chunk text, "snippet" for document-level FTS excerpts).
type SearchResult struct {
	ChunkID     int64   `json:"chunk_id"`
	DocumentID  int64   `json:"document_id"`
	Seq         int     `json:"seq"`
	Title       string  `json:"title"`
	Path        string  `json:"path"`
	Collection  string  `json:"collection"`
	Content     string  `json:"content"`
	ContentKind string  `json:"content_kind"`
	Score       float64 `json:"score"`
	ChunkType   int     `json:"chunk_type"`
	ImagePath   string  `json:"image_path,omitempty"`
	StartLine   int     `json:"start_line"`
	EndLine     int     `json:"end_line"`
}

// AggBucket is the wire form of one aggregation bucket.
type AggBucket struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// SearchOutput is the `seek search --json` envelope. The MCP seek_search
// tool deliberately does not use this envelope: it marshals []SearchResult
// directly (a shipped contract its clients rely on).
type SearchOutput struct {
	Query   string                 `json:"query"`
	Total   int                    `json:"total"`
	Results []SearchResult         `json:"results"`
	Aggs    map[string][]AggBucket `json:"aggs,omitempty"`
}

// AutocompleteOutput is the shared {query,suggestions} shape used by
// `seek search --autocomplete --json` and the MCP seek_autocomplete tool.
type AutocompleteOutput struct {
	Query       string   `json:"query"`
	Suggestions []string `json:"suggestions"`
}

// NewSearchResults maps domain hits to wire form, deriving content_kind.
// The result is always non-nil so JSON marshals as [] for empty input,
// never null.
func NewSearchResults(results []Result) []SearchResult {
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		out = append(out, SearchResult{
			ChunkID:     r.ChunkID,
			DocumentID:  r.DocumentID,
			Seq:         r.Seq,
			Title:       r.Title,
			Path:        r.Path,
			Collection:  r.Collection,
			Content:     r.Content,
			ContentKind: ContentKind(r),
			Score:       r.Score,
			ChunkType:   int(r.ChunkType),
			ImagePath:   r.ImagePath,
			StartLine:   r.StartLine,
			EndLine:     r.EndLine,
		})
	}
	return out
}

// NewAggBuckets maps domain buckets to wire form. Non-nil for non-nil input.
func NewAggBuckets(buckets []Bucket) []AggBucket {
	out := make([]AggBucket, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, AggBucket{Key: b.Key, Count: b.Count})
	}
	return out
}

// NewSearchOutput builds the `seek search --json` envelope. Results is never
// nil ([] for empty input); Aggs is nil — omitted via omitempty — when there
// are no aggregations.
func NewSearchOutput(query string, results []Result, aggs map[string][]Bucket) *SearchOutput {
	out := &SearchOutput{
		Query:   query,
		Total:   len(results),
		Results: NewSearchResults(results),
	}
	if len(aggs) > 0 {
		out.Aggs = make(map[string][]AggBucket, len(aggs))
		for spec, buckets := range aggs {
			out.Aggs[spec] = NewAggBuckets(buckets)
		}
	}
	return out
}

// NewAutocompleteOutput builds the shared autocomplete shape.
func NewAutocompleteOutput(query string, suggestions []string) *AutocompleteOutput {
	return &AutocompleteOutput{Query: query, Suggestions: suggestions}
}
