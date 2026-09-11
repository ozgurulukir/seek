package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ozgurulukir/seek/internal/app"
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
	Collection string   `help:"Filter by collection name"`
	Repo       string   `help:"Filter by repository or collection name (alias for --collection)"`
	DocType    string   `help:"Filter by document type (markdown, claude, codex, images, pdf, documents, parser, code)"`
	Lang       string   `help:"Filter code documents by programming language (e.g. go, python, typescript)"`
	After      string   `help:"Filter documents after this date (RFC3339)"`
	Before     string   `help:"Filter documents before this date (RFC3339)"`
	ChunkType  string   `help:"Filter by chunk type (text, image)"`
	Path       string   `help:"Filter by path pattern (GLOB)"`
	Workspace  string   `help:"Filter parser collections by workspace directory (fast field)"`
	Field      []string `help:"Filter by fast field name:value (e.g. topics:concurrency, entities:ORG:OpenAI, language:en, repo:myproject)"`
	Context    int      `short:"C" default:"0" help:"Number of surrounding chunks before and after to expand context"`

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

func (c *SearchCmd) Run(cfg *config.AppConfig) (err error) {
	ctx := context.Background()

	// Handle analyze mode
	if c.Analyze {
		return c.runAnalyze(cfg)
	}

	// Handle autocomplete mode
	if c.Autocomplete {
		return c.runAutocomplete(cfg)
	}

	runtime, err := app.Open(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := runtime.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	for _, warning := range runtime.Warnings {
		fmt.Fprintf(os.Stderr, "WARN: %s\n", warning)
	}

	runtime.Search.WithLogger(searchLogger{})

	req := c.searchRequest()
	results, err := runtime.RunSearch(ctx, req)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	// JSON mode: machine-readable output for agents; no pretty printing, no
	// context expansion. Content is emitted in full (the FTS snippet carries
	// >>> markers and 40-token truncation; agents decide how much to read).
	if c.JSON {
		if err := runtime.Search.EnrichContent(ctx, results); err != nil {
			return fmt.Errorf("enrich results: %w", err)
		}
		aggs, err := runtime.RunAggs(ctx, req)
		if err != nil {
			return fmt.Errorf("aggregations: %w", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(search.NewSearchOutput(c.Query, results, aggs))
	}

	// Run aggregations if requested
	if len(c.Aggs) > 0 {
		aggs, err := runtime.RunAggs(ctx, req)
		if err != nil {
			return fmt.Errorf("aggregations: %w", err)
		}
		fmt.Println("\nAggregations:")
		for spec, buckets := range aggs {
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

	expandContext(runtime.Store, c.Context, results)
	c.printResults(results)

	return nil
}

// searchRequest maps the CLI flags onto the surface-neutral request. Request
// policy (filter semantics, analyzer gating, RRFK, dispatch) lives in the
// app planner so the MCP tool shares it unchanged.
func (c *SearchCmd) searchRequest() app.SearchRequest {
	req := app.SearchRequest{
		Query:       c.Query,
		Limit:       c.Limit,
		Collection:  c.Collection,
		Repo:        c.Repo,
		DocType:     c.DocType,
		Lang:        c.Lang,
		After:       c.After,
		Before:      c.Before,
		ChunkType:   c.ChunkType,
		Path:        c.Path,
		Workspace:   c.Workspace,
		Fields:      c.Field,
		SortBy:      c.SortBy,
		SortOrder:   c.SortOrder,
		QueryMode:   c.QueryMode,
		AnalyzeLang: c.AnalyzeLang,
		Aggs:        c.Aggs,
	}
	switch {
	case c.Lex:
		req.Mode = app.ModeLex
	case c.Vec:
		req.Mode = app.ModeVec
	}
	return req
}

type searchLogger struct{}

func (searchLogger) Printf(format string, v ...interface{}) {
	fmt.Fprintf(os.Stderr, format, v...)
}

// runAnalyze handles the --analyze flag: tokenizes and stems the query text.
func (c *SearchCmd) runAnalyze(cfg *config.AppConfig) error {
	lang := app.EffectiveAnalyzeLang(c.AnalyzeLang, cfg)
	analyzer := search.NewAnalyzer(lang, true, true)
	tokens := analyzer.Analyze(c.Query)
	fmt.Printf("Analyzed (%s): %v\n", lang, tokens)
	return nil
}

// runAutocomplete handles the --autocomplete flag: shows prefix completions.
func (c *SearchCmd) runAutocomplete(cfg *config.AppConfig) (err error) {
	db, err := app.OpenStore(cfg)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()

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
		return enc.Encode(search.NewAutocompleteOutput(c.Query, results))
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

func (c *SearchCmd) printResults(results []search.Result) {
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
		if r.ChunkType == search.ChunkTypeImage && r.ImagePath != "" {
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

// expandContext expands each hit with surrounding chunks, shared by the CLI
// human output and the MCP context argument. Applied after EnrichContent;
// content_kind is unaffected (it is derived from ChunkID).
func expandContext(db *store.Store, radius int, results []search.Result) {
	if radius <= 0 {
		return
	}
	for idx := range results {
		if results[idx].DocumentID > 0 {
			expanded, sLine, eLine, err := db.GetSurroundingContext(results[idx].DocumentID, results[idx].Seq, radius)
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

func formatSnippet(content string, maxLen int) string {
	// Clean up whitespace
	s := strings.ReplaceAll(content, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}
