package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

type SearchCmd struct {
	Query string `arg:"" help:"Search query"`
	Lex   bool   `help:"BM25 full-text search only"`
	Vec   bool   `help:"Vector semantic search only"`
	Limit int    `short:"l" default:"10" help:"Max results"`

	// New filter flags
	Collection string `help:"Filter by collection name"`
	Repo       string `help:"Filter by repository or collection name (alias for --collection)"`
	DocType    string `help:"Filter by document type (markdown, claude, codex, images, pdf, documents, parser, code)"`
	Lang       string `help:"Filter code documents by programming language (e.g. go, python, typescript)"`
	After      string `help:"Filter documents after this date (RFC3339)"`
	Before     string `help:"Filter documents before this date (RFC3339)"`
	ChunkType  string `help:"Filter by chunk type (text, image)"`
	Path       string `help:"Filter by path pattern (GLOB)"`
	Workspace  string `help:"Filter parser collections by workspace directory (fast field)"`
	Context    int    `short:"C" default:"0" help:"Number of surrounding chunks before and after to expand context"`

	// Aggregation flags
	Aggs []string `help:"Aggregations to run (e.g., type:terms, created_at:histogram:month)"`

	// Query mode
	QueryMode string `help:"Query mode: raw or parsed" default:""`

	// Sorting
	SortBy    string `help:"Sort results by field (e.g., created_at, line_count)"`
	SortOrder string `help:"Sort order: asc or desc" default:"desc"`

	// Analysis
	Analyze         bool   `help:"Analyze query text (tokenize, stem) and exit"`
	AnalyzeLang     string `help:"Language for analysis (en, tr); defaults to search.analyze_lang in config, then en"`
	Autocomplete    bool   `help:"Show autocomplete suggestions for the query prefix"`
	AutocompleteMax int    `help:"Max autocomplete suggestions" default:"10"`
	JSON            bool   `help:"Emit machine-readable JSON instead of human-formatted output"`
}

func (c *SearchCmd) Run(cfg *config.AppConfig) error {
	ctx := context.Background()

	// Handle analyze mode
	if c.Analyze {
		return c.runAnalyze(cfg)
	}

	// Handle autocomplete mode
	if c.Autocomplete {
		return c.runAutocomplete(cfg)
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	// Set up vector index if configured
	if cfg.Config.VectorIndex.Backend != "" && cfg.Config.VectorIndex.Backend != "linear" {
		vi, err := store.NewVectorIndex(cfg)
		if err == nil {
			db.SetVectorIndex(vi)
		}
	}

	// Set up compression
	db.SetCompression(cfg.Config.Compression.Algorithm != "none", cfg.Config.Compression.Level)

	engine, embedClient, vlClient := c.buildEngine(db, cfg)
	filters := c.buildFilters()

	// Build analyzer if tokenization is enabled
	var analyzer *search.Analyzer
	if c.QueryMode != "raw" && cfg.Config.Search.QueryMode != "raw" {
		analyzer = search.NewAnalyzer(effectiveAnalyzeLang(c.AnalyzeLang, cfg), true, true)
	}

	opts := search.Options{
		Filters:      filters,
		Aggregations: c.Aggs,
		QueryMode:    c.QueryMode,
		Limit:        c.Limit,
		RRFK:         cfg.Config.Search.RRFK,
		SortBy:       c.SortBy,
		SortOrder:    c.SortOrder,
		Analyzer:     analyzer,
	}

	results, err := c.executeSearch(ctx, engine, embedClient, vlClient, opts)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	// JSON mode: machine-readable output for agents; no pretty printing, no
	// context expansion. Content is emitted in full (the FTS snippet carries
	// >>> markers and 40-token truncation; agents decide how much to read).
	if c.JSON {
		enrichJSONContent(db, results)
		aggs, err := c.computeAggregations(ctx, engine, filters)
		if err != nil {
			return fmt.Errorf("aggregations: %w", err)
		}
		return c.printResultsJSON(results, aggs)
	}

	// Run aggregations if requested
	if len(c.Aggs) > 0 {
		if err := c.printAggregations(ctx, engine, filters); err != nil {
			return fmt.Errorf("aggregations: %w", err)
		}
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	c.expandContext(db, results)
	c.printResults(results)

	return nil
}

type searchLogger struct{}

func (searchLogger) Printf(format string, v ...interface{}) {
	fmt.Fprintf(os.Stderr, format, v...)
}

func (c *SearchCmd) buildEngine(db *store.Store, cfg *config.AppConfig) (*search.Engine, *embed.Client, *embed.VLClient) {
	embedClient := newEmbedClient(cfg)
	vlClient := newVLClient(cfg)

	var engine *search.Engine
	if vlClient != nil {
		engine = search.NewEngineWithVL(db, embedClient, vlClient)
	} else {
		engine = search.NewEngine(db, embedClient)
	}
	engine.WithLogger(searchLogger{})

	if cfg.Config.Rerank.Enabled && cfg.Config.Rerank.APIKey != "" && !cfg.Config.OfflineOnly() {
		reranker := embed.NewRerankClient(cfg.Config.Rerank.BaseURL, cfg.Config.Rerank.APIKey, cfg.Config.Rerank.Model)
		engine.WithReranker(reranker)
	}

	return engine, embedClient, vlClient
}

func (c *SearchCmd) buildFilters() *store.FilterSet {
	colName := c.Collection
	if colName == "" {
		colName = c.Repo
	}

	if colName == "" && c.DocType == "" && c.Lang == "" && c.After == "" && c.Before == "" && c.ChunkType == "" && c.Path == "" && c.Workspace == "" {
		return nil
	}

	filters := store.NewFilterSet()
	if colName != "" {
		filters.Add(&store.CollectionFilter{Name: colName})
	}
	if c.DocType != "" {
		filters.Add(&store.DocTypeFilter{Type: c.DocType})
	}
	if c.Lang != "" {
		filters.Add(&store.FastFieldFilter{Field: "lang", Value: strings.ToLower(c.Lang)})
	}
	if c.After != "" || c.Before != "" {
		filters.Add(&store.DateRangeFilter{After: c.After, Before: c.Before})
	}
	if c.ChunkType != "" {
		ct := 0
		if strings.ToLower(c.ChunkType) == "image" {
			ct = 1
		}
		filters.Add(&store.ChunkTypeFilter{Type: ct})
	}
	if c.Path != "" {
		filters.Add(&store.PathFilter{Pattern: c.Path})
	}
	if c.Workspace != "" {
		filters.Add(&store.FastFieldFilter{Field: "workspace", Value: c.Workspace})
	}

	return filters
}

func (c *SearchCmd) executeSearch(ctx context.Context, engine *search.Engine, embedClient *embed.Client, vlClient *embed.VLClient, opts search.Options) ([]store.SearchResult, error) {
	switch {
	case c.Lex:
		return engine.SearchBM25(ctx, c.Query, c.Limit, opts)
	case c.Vec:
		if embedClient == nil && vlClient == nil {
			return nil, fmt.Errorf("vector search requires embedding API key")
		}
		return engine.SearchVector(ctx, c.Query, c.Limit, opts)
	default:
		return engine.SearchHybrid(ctx, c.Query, c.Limit, opts)
	}
}

func (c *SearchCmd) printAggregations(ctx context.Context, engine *search.Engine, filters *store.FilterSet) error {
	aggResults, err := engine.RunAggregations(ctx, c.Aggs, filters)
	if err != nil {
		return err
	}
	fmt.Println("\nAggregations:")
	for spec, buckets := range aggResults {
		fmt.Printf("  %s:\n", spec)
		for _, b := range buckets {
			fmt.Printf("    %s: %d\n", b.Key, b.Count)
		}
	}
	fmt.Println()
	return nil
}

// computeAggregations runs the requested aggregations without printing, for
// the JSON output path. Empty spec list returns nil (no aggregations block).
func (c *SearchCmd) computeAggregations(ctx context.Context, engine *search.Engine, filters *store.FilterSet) (map[string][]search.Bucket, error) {
	if len(c.Aggs) == 0 {
		return nil, nil
	}
	return engine.RunAggregations(ctx, c.Aggs, filters)
}

// enrichJSONContent replaces FTS highlight snippets with the full chunk
// content for JSON output. Best-effort: a missing chunk keeps its snippet
// (with the >>>/<<< markers stripped). db may be nil in tests.
func enrichJSONContent(db *store.Store, results []store.SearchResult) {
	for i := range results {
		if results[i].ChunkID <= 0 {
			results[i].Content = strings.ReplaceAll(results[i].Content, ">>>", "")
			results[i].Content = strings.ReplaceAll(results[i].Content, "<<<", "")
			continue
		}
		if content, err := db.GetChunkContent(results[i].ChunkID); err == nil {
			results[i].Content = content
		}
	}
}

// jsonSearchResult mirrors store.SearchResult with explicit, stable JSON
// field names for agent consumption.
type jsonSearchResult struct {
	ChunkID    int64   `json:"chunk_id"`
	DocumentID int64   `json:"document_id"`
	Seq        int     `json:"seq"`
	Title      string  `json:"title"`
	Path       string  `json:"path"`
	Collection string  `json:"collection"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
	ChunkType  int     `json:"chunk_type"`
	ImagePath  string  `json:"image_path,omitempty"`
	StartLine  int     `json:"start_line"`
	EndLine    int     `json:"end_line"`
}

// jsonSearchOutput is the top-level --json envelope.
type jsonSearchOutput struct {
	Query   string                     `json:"query"`
	Total   int                        `json:"total"`
	Results []jsonSearchResult         `json:"results"`
	Aggs    map[string][]jsonAggBucket `json:"aggs,omitempty"`
}

type jsonAggBucket struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

func (c *SearchCmd) printResultsJSON(results []store.SearchResult, aggs map[string][]search.Bucket) error {
	out := buildJSONOutput(c, results, aggs)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// buildJSONOutput maps search results and aggregations into the stable JSON
// envelope; separated from printing so it is directly testable.
func buildJSONOutput(c *SearchCmd, results []store.SearchResult, aggs map[string][]search.Bucket) *jsonSearchOutput {
	out := jsonSearchOutput{
		Query:   c.Query,
		Total:   len(results),
		Results: make([]jsonSearchResult, 0, len(results)),
	}
	for _, r := range results {
		out.Results = append(out.Results, jsonSearchResult{
			ChunkID:    r.ChunkID,
			DocumentID: r.DocumentID,
			Seq:        r.Seq,
			Title:      r.Title,
			Path:       r.Path,
			Collection: r.Collection,
			Content:    r.Content,
			Score:      r.Score,
			ChunkType:  int(r.ChunkType),
			ImagePath:  r.ImagePath,
			StartLine:  r.StartLine,
			EndLine:    r.EndLine,
		})
	}
	if aggs != nil {
		out.Aggs = make(map[string][]jsonAggBucket, len(aggs))
		for spec, buckets := range aggs {
			jb := make([]jsonAggBucket, 0, len(buckets))
			for _, b := range buckets {
				jb = append(jb, jsonAggBucket{Key: b.Key, Count: b.Count})
			}
			out.Aggs[spec] = jb
		}
	}
	return &out
}

func (c *SearchCmd) expandContext(db *store.Store, results []store.SearchResult) {
	if c.Context <= 0 {
		return
	}
	for idx := range results {
		if results[idx].DocumentID > 0 {
			expanded, sLine, eLine, err := db.GetSurroundingContext(results[idx].DocumentID, results[idx].Seq, c.Context)
			if err == nil && expanded != "" {
				results[idx].Content = expanded
				if sLine > 0 {
					results[idx].StartLine = sLine
				}
				if eLine > 0 {
					results[idx].EndLine = eLine
				}
			}
		}
	}
}

func (c *SearchCmd) printResults(results []store.SearchResult) {
	for i, r := range results {
		pathLoc := formatRelPath(r.Path)
		if r.StartLine > 0 {
			if r.EndLine > r.StartLine {
				pathLoc = fmt.Sprintf("%s:L%d-L%d", pathLoc, r.StartLine, r.EndLine)
			} else {
				pathLoc = fmt.Sprintf("%s:L%d", pathLoc, r.StartLine)
			}
		}

		fmt.Printf("\n%s %s\n", fmt.Sprintf("[%d]", i+1), r.Title)
		fmt.Printf("    %s  (%s)  score=%.4f\n", pathLoc, r.Collection, r.Score)
		if r.ChunkType == store.ChunkTypeImage && r.ImagePath != "" {
			fmt.Printf("    %s\n", formatRelPath(r.ImagePath))
			if r.Content != "" {
				snippet := formatSnippet(r.Content, config.DefaultImageSnippetLen)
				fmt.Printf("    context: %s\n", snippet)
			}
		} else if r.Content != "" {
			maxSnippetLen := config.DefaultTextSnippetLen
			if c.Context > 0 {
				ctxVal := c.Context
				if ctxVal > 50 {
					ctxVal = 50
				}
				maxSnippetLen = config.DefaultTextSnippetLen * (ctxVal*2 + 1)
				const maxAllowedSnippetLen = 10000
				if maxSnippetLen > maxAllowedSnippetLen {
					maxSnippetLen = maxAllowedSnippetLen
				}
			}
			snippet := formatSnippet(r.Content, maxSnippetLen)
			fmt.Printf("    %s\n", snippet)
		}
	}
	fmt.Println()
}

// runAnalyze handles the --analyze flag: tokenizes and stems the query text.
func (c *SearchCmd) runAnalyze(cfg *config.AppConfig) error {
	lang := effectiveAnalyzeLang(c.AnalyzeLang, cfg)
	analyzer := search.NewAnalyzer(lang, true, true)
	tokens := analyzer.Analyze(c.Query)
	fmt.Printf("Analyzed (%s): %v\n", lang, tokens)
	return nil
}

// effectiveAnalyzeLang resolves the analysis language: an explicit language
// flag wins, then search.analyze_lang from config, then the built-in default
// ("en"). Shared by `seek search --analyze` and `seek analyze`.
func effectiveAnalyzeLang(flagLang string, cfg *config.AppConfig) string {
	if flagLang != "" {
		return flagLang
	}
	if cfg.Config.Search.AnalyzeLang != "" {
		return cfg.Config.Search.AnalyzeLang
	}
	return config.DefaultAnalyzeLang
}

// runAutocomplete handles the --autocomplete flag: shows prefix completions.
func (c *SearchCmd) runAutocomplete(cfg *config.AppConfig) error {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	query := strings.TrimSpace(c.Query)
	var results []string

	// Support multi-word queries by completing the last word while preserving prefix
	lastSpace := strings.LastIndex(query, " ")
	if lastSpace >= 0 {
		lead := query[:lastSpace+1]
		word := query[lastSpace+1:]
		if word != "" {
			completions, err := db.AutocompleteTerms(word, c.AutocompleteMax)
			if err != nil {
				return fmt.Errorf("autocomplete: %w", err)
			}
			for _, comp := range completions {
				results = append(results, lead+comp)
			}
		}
	} else {
		var err error
		results, err = db.AutocompleteTerms(query, c.AutocompleteMax)
		if err != nil {
			return fmt.Errorf("autocomplete: %w", err)
		}
	}

	if c.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(&struct {
			Query       string   `json:"query"`
			Suggestions []string `json:"suggestions"`
		}{Query: c.Query, Suggestions: results})
	}

	if len(results) == 0 {
		fmt.Println("No suggestions found.")
		return nil
	}

	fmt.Printf("Suggestions for %q:\n", c.Query)
	for _, r := range results {
		fmt.Printf("  %s\n", r)
	}
	return nil
}

func formatSnippet(content string, maxLen int) string {
	// Clean up whitespace
	s := strings.ReplaceAll(content, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}
