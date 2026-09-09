package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

// McpCmd runs an MCP (Model Context Protocol) server on stdio, exposing the
// seek index as tools for AI agents. It reads only the local SQLite database
// and never performs network calls itself; embedding-backed vector search
// reaches the configured provider exactly like `seek search` does. The
// privacy.offline_only setting applies unchanged.
type McpCmd struct{}

type mcpSearchArgs struct {
	Query      string `json:"query" jsonschema:"the search query (required)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max results to return (default 10)"`
	Lex        bool   `json:"lex,omitempty" jsonschema:"BM25 full-text search only"`
	Vec        bool   `json:"vec,omitempty" jsonschema:"vector semantic search only"`
	Collection string `json:"collection,omitempty" jsonschema:"filter by collection name"`
}

// maxMCPResults caps how many results a single MCP search may return, so a
// hostile or buggy client cannot force the server to materialize the whole
// index (huge JSON, memory pressure).
const maxMCPResults = 100

// mcpSearchResult mirrors the `seek search --json` result fields so agents
// get the same shape over both surfaces.
type mcpSearchResult struct {
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

func (c *McpCmd) Run(cfg *config.AppConfig) error {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	server, err := buildMCPServer(db, cfg)
	if err != nil {
		return err
	}

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}

// buildMCPServer wires the store and search engine into an MCP server with
// the three seek tools. Separated from Run so tests can drive it over an
// in-memory transport.
func buildMCPServer(db *store.Store, cfg *config.AppConfig) (*mcp.Server, error) {
	if cfg.Config.VectorIndex.Backend != "" && cfg.Config.VectorIndex.Backend != "linear" {
		if vi, err := store.NewVectorIndex(cfg); err == nil {
			db.SetVectorIndex(vi)
		}
	}
	db.SetCompression(cfg.Config.Compression.Algorithm != "", cfg.Config.Compression.Level)

	embedClient := embed.NewClientFromConfig(cfg)
	vlClient := embed.NewVLClientFromConfig(cfg)
	var engine *search.Engine
	if vlClient != nil {
		engine = search.NewEngineWithVL(db, embedClient, vlClient)
	} else {
		engine = search.NewEngine(db, embedClient)
	}
	engine.WithLogger(mcpLogger{})

	server := mcp.NewServer(&mcp.Implementation{Name: "seek", Version: "dev"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "seek_search",
		Description: "Hybrid search over the seek index (BM25 + vector + RRF fusion). Returns ranked results with chunk content, scores, and line spans. Fields match `seek search --json`.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpSearchArgs) (*mcp.CallToolResult, any, error) {
		limit := args.Limit
		if limit <= 0 {
			limit = 10
		} else if limit > maxMCPResults {
			limit = maxMCPResults
		}
		results, err := runMCPSearch(ctx, engine, cfg, &args, limit)
		if err != nil {
			return nil, nil, err
		}
		// Parity with `seek search --json` (single source of truth in
		// internal/search/quality.go): chunk-level hits get full content,
		// document-level hits get markers stripped.
		enrichJSONContent(db, results)
		out := make([]mcpSearchResult, 0, len(results))
		for _, r := range results {
			mr := mcpSearchResult{
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
			}
			mr.ContentKind = mcpContentKind(&mr)
			out = append(out, mr)
		}
		payload, err := json.Marshal(out)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "seek_status",
		Description: "List indexed collections with document and chunk counts.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		collections, err := db.ListCollections()
		if err != nil {
			return nil, nil, err
		}
		type colInfo struct {
			Name      string `json:"name"`
			Type      string `json:"type"`
			Documents int    `json:"documents"`
			Chunks    int    `json:"chunks"`
		}
		out := make([]colInfo, 0, len(collections))
		for _, col := range collections {
			docs, err := db.CountDocuments(col.ID)
			if err != nil {
				return nil, nil, err
			}
			chunks, err := db.CountChunks(col.ID)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, colInfo{Name: col.Name, Type: string(col.Type), Documents: docs, Chunks: chunks})
		}
		payload, err := json.Marshal(out)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "seek_autocomplete",
		Description: "Prefix-completion suggestions from the indexed vocabulary. Useful for query refinement.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		Prefix string `json:"prefix" jsonschema:"the prefix to complete"`
		Max    int    `json:"max,omitempty" jsonschema:"max suggestions (default 10)"`
	}) (*mcp.CallToolResult, any, error) {
		max := args.Max
		if max <= 0 {
			max = 10
		}
		terms, err := db.AutocompleteTerms(strings.TrimSpace(args.Prefix), max)
		if err != nil {
			return nil, nil, err
		}
		payload, err := json.Marshal(map[string]any{"query": args.Prefix, "suggestions": terms})
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		}, nil, nil
	})

	return server, nil
}

// runMCPSearch dispatches like SearchCmd.executeSearch: lex, vec, or hybrid.
func runMCPSearch(ctx context.Context, engine *search.Engine, cfg *config.AppConfig, args *mcpSearchArgs, limit int) ([]store.SearchResult, error) {
	filters := store.NewFilterSet()
	if args.Collection != "" {
		filters.Add(&store.CollectionFilter{Name: args.Collection})
	}
	// Analyzer mirrors SearchCmd: tokenization unless query mode is "raw".
	var analyzer *search.Analyzer
	if cfg.Config.Search.QueryMode != "raw" {
		analyzer = search.NewAnalyzer(effectiveAnalyzeLang("", cfg), true, true)
	}
	opts := search.Options{
		Filters:   filters,
		QueryMode: cfg.Config.Search.QueryMode,
		Limit:     limit,
		RRFK:      cfg.Config.Search.RRFK,
		Analyzer:  analyzer,
	}
	if opts.RRFK <= 0 {
		opts.RRFK = search.DefaultRRFK
	}
	switch {
	case args.Lex:
		return engine.SearchBM25(ctx, args.Query, limit, opts)
	case args.Vec:
		return engine.SearchVector(ctx, args.Query, limit, opts)
	default:
		return engine.SearchHybrid(ctx, args.Query, limit, opts)
	}
}

// mcpContentKind classifies content the same way `seek search --json` does.
// Classification single-sourced in internal/search (quality.go).
func mcpContentKind(m *mcpSearchResult) string {
	return search.ContentKind(store.SearchResult{ChunkID: m.ChunkID})
}

type mcpLogger struct{}

func (mcpLogger) Printf(format string, v ...interface{}) {
	// stdout carries the MCP protocol; diagnostics must go to stderr. The
	// engine already prefixes messages with "  WARN:" — pass through as-is.
	fmt.Fprintf(os.Stderr, format, v...)
}
