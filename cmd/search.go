package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
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
	DocType    string `help:"Filter by document type (markdown, claude, codex, images, pdf)"`
	After      string `help:"Filter documents after this date (RFC3339)"`
	Before     string `help:"Filter documents before this date (RFC3339)"`
	ChunkType  string `help:"Filter by chunk type (text, image)"`
	Path       string `help:"Filter by path pattern (GLOB)"`

	// Aggregation flags
	Aggs []string `help:"Aggregations to run (e.g., type:terms, created_at:histogram:month)"`

	// Query mode
	QueryMode string `help:"Query mode: raw or parsed" default:""`

	// Sorting
	SortBy    string `help:"Sort results by field (e.g., created_at, line_count)"`
	SortOrder string `help:"Sort order: asc or desc" default:"desc"`

	// Analysis
	Analyze         bool   `help:"Analyze query text (tokenize, stem) and exit"`
	AnalyzeLang     string `help:"Language for analysis (en, tr)" default:"en"`
	Autocomplete    bool   `help:"Show autocomplete suggestions for the query prefix"`
	AutocompleteMax int    `help:"Max autocomplete suggestions" default:"10"`
}

func (c *SearchCmd) Run(cfg *config.AppConfig) error {
	ctx := context.Background()

	// Handle analyze mode
	if c.Analyze {
		return c.runAnalyze()
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
			defer vi.Save(cfg.Config.VectorIndex.HNSW.PersistPath)
		}
	}

	// Set up compression
	db.SetCompression(cfg.Config.Compression.Algorithm != "none", cfg.Config.Compression.Level)

	embedClient := newEmbedClient(cfg)
	vlClient := newVLClient(cfg)

	var engine *search.Engine
	if vlClient != nil {
		engine = search.NewEngineWithVL(db, embedClient, vlClient)
	} else {
		engine = search.NewEngine(db, embedClient)
	}

	// Build filters
	var filters *store.FilterSet
	if c.Collection != "" || c.DocType != "" || c.After != "" || c.Before != "" || c.ChunkType != "" || c.Path != "" {
		filters = store.NewFilterSet()
		if c.Collection != "" {
			filters.Add(&store.CollectionFilter{Name: c.Collection})
		}
		if c.DocType != "" {
			filters.Add(&store.DocTypeFilter{Type: c.DocType})
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
	}

	// Build analyzer if tokenization is enabled
	var analyzer *search.Analyzer
	if c.QueryMode != "raw" && cfg.Config.Search.QueryMode != "raw" {
		analyzer = search.NewAnalyzer(c.AnalyzeLang, true, true)
	}

	opts := search.Options{
		Filters:      filters,
		Aggregations: c.Aggs,
		QueryMode:    c.QueryMode,
		Limit:        c.Limit,
		SortBy:       c.SortBy,
		SortOrder:    c.SortOrder,
		Analyzer:     analyzer,
	}

	var results []store.SearchResult

	switch {
	case c.Lex:
		results, err = engine.SearchBM25(ctx, c.Query, c.Limit, opts)
	case c.Vec:
		if embedClient == nil && vlClient == nil {
			return fmt.Errorf("vector search requires embedding API key")
		}
		results, err = engine.SearchVector(ctx, c.Query, c.Limit, opts)
	default:
		results, err = engine.SearchHybrid(ctx, c.Query, c.Limit, opts)
	}

	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	// Run aggregations if requested
	if len(c.Aggs) > 0 {
		aggResults, err := engine.RunAggregations(ctx, c.Aggs, filters)
		if err != nil {
			return fmt.Errorf("aggregations: %w", err)
		}
		fmt.Println("\nAggregations:")
		for spec, buckets := range aggResults {
			fmt.Printf("  %s:\n", spec)
			for _, b := range buckets {
				fmt.Printf("    %s: %d\n", b.Key, b.Count)
			}
		}
		fmt.Println()
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	for i, r := range results {
		fmt.Printf("\n%s %s\n", fmt.Sprintf("[%d]", i+1), r.Title)
		fmt.Printf("    %s  (%s)  score=%.4f\n", formatRelPath(r.Path), r.Collection, r.Score)
		if r.ChunkType == store.ChunkTypeImage && r.ImagePath != "" {
			fmt.Printf("    %s\n", formatRelPath(r.ImagePath))
			if r.Content != "" {
				snippet := formatSnippet(r.Content, config.DefaultImageSnippetLen)
				fmt.Printf("    context: %s\n", snippet)
			}
		} else if r.Content != "" {
			snippet := formatSnippet(r.Content, config.DefaultTextSnippetLen)
			fmt.Printf("    %s\n", snippet)
		}
	}
	fmt.Println()

	return nil
}

// runAnalyze handles the --analyze flag: tokenizes and stems the query text.
func (c *SearchCmd) runAnalyze() error {
	analyzer := search.NewAnalyzer(c.AnalyzeLang, true, true)
	tokens := analyzer.Analyze(c.Query)
	fmt.Printf("Analyzed (%s): %v\n", c.AnalyzeLang, tokens)
	return nil
}

// runAutocomplete handles the --autocomplete flag: shows prefix completions.
func (c *SearchCmd) runAutocomplete(cfg *config.AppConfig) error {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	// Set up compression
	db.SetCompression(cfg.Config.Compression.Algorithm != "none", cfg.Config.Compression.Level)

	// Collect terms from chunk content for autocomplete
	rows, err := db.DB().Query(`SELECT content, content_zstd FROM chunks WHERE embedding IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("query chunks: %w", err)
	}
	defer rows.Close()

	termSet := make(map[string]bool)
	for rows.Next() {
		var content string
		var contentZstd []byte
		if err := rows.Scan(&content, &contentZstd); err != nil {
			continue
		}
		if len(contentZstd) > 0 {
			content, err = store.DecompressString(contentZstd)
			if err != nil {
				continue
			}
		}
		tokens := search.AnalyzeToken(content)
		for _, t := range tokens {
			if len(t) >= 2 {
				termSet[t] = true
			}
		}
	}

	terms := make([]string, 0, len(termSet))
	for t := range termSet {
		terms = append(terms, t)
	}

	ac, err := search.NewAutocomplete(terms)
	if err != nil {
		return fmt.Errorf("build autocomplete: %w", err)
	}
	defer ac.Close()

	results := ac.Complete(c.Query, c.AutocompleteMax)
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
