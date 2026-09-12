package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

// McpCmd runs an MCP (Model Context Protocol) server on stdio, exposing the
// seek index as tools for AI agents. It reads only the local SQLite database
// and never performs network calls itself; embedding-backed vector search
// reaches the configured provider exactly like `seek search` does. The
// privacy.offline_only setting applies unchanged.
type McpCmd struct{}

// mcpSearchArgs mirrors the SearchRequest filter slots so agents get the
// same capabilities as `seek search`. Request policy lives in the shared
// app planner; this struct is transport-only.
type mcpSearchArgs struct {
	Query      string   `json:"query" jsonschema:"the search query (required)"`
	Limit      int      `json:"limit,omitempty" jsonschema:"max results to return, capped at 100 (default 10)"`
	Lex        bool     `json:"lex,omitempty" jsonschema:"BM25 full-text search only"`
	Vec        bool     `json:"vec,omitempty" jsonschema:"vector semantic search only (requires a configured embedding API key)"`
	Collection string   `json:"collection,omitempty" jsonschema:"filter by collection name"`
	Repo       string   `json:"repo,omitempty" jsonschema:"filter by repository or collection name (alias of collection; setting both ANDs them)"`
	DocType    string   `json:"doc_type,omitempty" jsonschema:"filter by document type (markdown, claude, codex, images, pdf, documents, parser, code)"`
	Lang       string   `json:"lang,omitempty" jsonschema:"filter code documents by programming language (e.g. go, python, typescript)"`
	Tag        string   `json:"tag,omitempty" jsonschema:"filter documents by tag"`
	After      string   `json:"after,omitempty" jsonschema:"only documents dated after this RFC3339 timestamp"`
	Before     string   `json:"before,omitempty" jsonschema:"only documents dated before this RFC3339 timestamp"`
	ChunkType  string   `json:"chunk_type,omitempty" jsonschema:"filter by chunk type: text (default) or image"`
	Path       string   `json:"path,omitempty" jsonschema:"filter by path GLOB pattern (e.g. notes/**/*.md)"`
	Workspace  string   `json:"workspace,omitempty" jsonschema:"filter parser collections by workspace directory"`
	Field      []string `json:"field,omitempty" jsonschema:"fast-field filters as name:value strings (e.g. topics:concurrency, entities:ORG:OpenAI); discover names and values with seek_fields"`
	SortBy     string   `json:"sort_by,omitempty" jsonschema:"sort results by a field (e.g. created_at, line_count, title) instead of relevance"`
	SortOrder  string   `json:"sort_order,omitempty" jsonschema:"sort direction with sort_by: asc or desc (default desc)"`
	Context    int      `json:"context,omitempty" jsonschema:"expand each hit with N surrounding chunks (0 = off; raises start_line/end_line spans)"`
	Aggs       []string `json:"aggs,omitempty" jsonschema:"aggregations to compute alongside the search (for example doc_type terms, lang terms, created_at histogram month); terms work on any indexed fast field discoverable via seek_fields; returned as a second text block"`
}

// mcpFieldsArgs drives seek_fields: no field → summary of all fields;
// field → distinct values with counts (mirrors `seek fields`).
type mcpFieldsArgs struct {
	Field      string `json:"field,omitempty" jsonschema:"fast field to inspect (e.g. tags, topics, lang); omit for a summary of all fields"`
	Collection string `json:"collection,omitempty" jsonschema:"limit results to a collection name"`
	Prefix     string `json:"prefix,omitempty" jsonschema:"case-insensitive value prefix filter (field mode only)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max values in field mode (default 50)"`
}

// maxMCPResults caps how many results a single MCP search may return, so a
// hostile or buggy client cannot force the server to materialize the whole
// index (huge JSON, memory pressure).
const maxMCPResults = 100

func (c *McpCmd) Run(cfg *config.AppConfig) (err error) {
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

	server, err := buildMCPServerWithServices(runtime, cfg)
	if err != nil {
		return err
	}

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}

// buildMCPServer wires a bare Store into an MCP server. Separated from Run
// so tests can drive it over an in-memory transport. The runtime it builds
// is deliberately degraded: it has no embedding clients, so vector search
// fails the same way an unconfigured real run does.
func buildMCPServer(db *store.Store, cfg *config.AppConfig) (*mcp.Server, error) {
	if _, _, err := app.ConfigureVectorIndex(context.Background(), db, cfg); err != nil {
		return nil, err
	}
	db.ConfigureCompression(cfg.Config.Compression)
	engine, err := app.NewSearchEngine(db, cfg)
	if err != nil {
		return nil, err
	}
	return buildMCPServerWithServices(&app.Runtime{Store: db, Search: engine}, cfg)
}

func buildMCPServerWithServices(runtime *app.Runtime, cfg *config.AppConfig) (*mcp.Server, error) {
	runtime.Search.WithLogger(mcpLogger{})

	server := mcp.NewServer(&mcp.Implementation{Name: "seek", Version: "dev"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "seek_search",
		Description: "Hybrid search over the seek index (BM25 + vector + RRF fusion). Returns ranked results with chunk content, scores, and line spans. " +
			"Fields match `seek search --json`. When aggs is passed, the response carries a second text block with aggregation buckets keyed by spec.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpSearchArgs) (*mcp.CallToolResult, any, error) {
		limit := args.Limit
		if limit <= 0 {
			limit = 10
		} else if limit > maxMCPResults {
			limit = maxMCPResults
		}
		searchArgs := args
		searchArgs.Limit = limit
		results, err := runtime.RunSearch(ctx, searchArgs.searchRequest())
		if err != nil {
			return nil, nil, err
		}
		// Parity with `seek search --json`: chunk-level hits get full
		// content, document-level hits get markers stripped, then optional
		// context expansion widens the line spans.
		if err := runtime.Search.EnrichContent(ctx, results); err != nil {
			return nil, nil, fmt.Errorf("enrich results: %w", err)
		}
		expandContext(runtime.Store, args.Context, results)
		payload, err := json.Marshal(search.NewSearchResults(results))
		if err != nil {
			return nil, nil, err
		}
		content := []mcp.Content{&mcp.TextContent{Text: string(payload)}}

		if len(args.Aggs) > 0 {
			aggs, err := runtime.RunAggs(ctx, searchArgs.searchRequest())
			if err != nil {
				return nil, nil, fmt.Errorf("aggregations: %w", err)
			}
			wire := make(map[string][]search.AggBucket, len(aggs))
			for spec, buckets := range aggs {
				wire[spec] = search.NewAggBuckets(buckets)
			}
			aggPayload, err := json.Marshal(wire)
			if err != nil {
				return nil, nil, err
			}
			content = append(content, &mcp.TextContent{Text: string(aggPayload)})
		}
		return &mcp.CallToolResult{Content: content}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "seek_fields",
		Description: "Inspect fast-field metadata: omit field for a summary of all fields (names, match modes, coverage), or pass a field to list its distinct values with document counts. Use the discovered name:value pairs with seek_search field filters.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpFieldsArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Field) == "" {
			resp, err := buildFieldsSummary(runtime.Store, args.Collection)
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(resp)
			if err != nil {
				return nil, nil, err
			}
			return textResult(payload)
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 50
		}
		values, err := listFieldValues(ctx, runtime.Store, args.Field, args.Collection, args.Prefix, limit)
		if err != nil {
			return nil, nil, err
		}
		payload, err := json.Marshal(values)
		if err != nil {
			return nil, nil, err
		}
		return textResult(payload)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "seek_status",
		Description: "List indexed collections with document and chunk counts.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		collections, err := runtime.Store.ListCollections()
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
			docs, err := runtime.Store.CountDocuments(col.ID)
			if err != nil {
				return nil, nil, err
			}
			chunks, err := runtime.Store.CountChunks(col.ID)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, colInfo{Name: col.Name, Type: string(col.Type), Documents: docs, Chunks: chunks})
		}
		payload, err := json.Marshal(out)
		if err != nil {
			return nil, nil, err
		}
		return textResult(payload)
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
		terms, err := runtime.Store.AutocompleteTerms(strings.TrimSpace(args.Prefix), max)
		if err != nil {
			return nil, nil, err
		}
		payload, err := json.Marshal(search.NewAutocompleteOutput(args.Prefix, terms))
		if err != nil {
			return nil, nil, err
		}
		return textResult(payload)
	})

	return server, nil
}

// searchRequest maps MCP tool arguments onto the shared request. Zero-value
// fields fall back to config defaults in the planner, exactly like unset CLI
// flags do.
func (a *mcpSearchArgs) searchRequest() app.SearchRequest {
	req := app.SearchRequest{
		Query:      a.Query,
		Limit:      a.Limit,
		Collection: a.Collection,
		Repo:       a.Repo,
		DocType:    a.DocType,
		Lang:       a.Lang,
		Tag:        a.Tag,
		After:      a.After,
		Before:     a.Before,
		ChunkType:  a.ChunkType,
		Path:       a.Path,
		Workspace:  a.Workspace,
		Fields:     a.Field,
		SortBy:     a.SortBy,
		SortOrder:  a.SortOrder,
		Aggs:       a.Aggs,
	}
	// Lex wins over Vec when both are set (same precedence as the CLI).
	switch {
	case a.Lex:
		req.Mode = app.ModeLex
	case a.Vec:
		req.Mode = app.ModeVec
	}
	return req
}

func textResult(payload []byte) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
	}, nil, nil
}

type mcpLogger struct{}

func (mcpLogger) Printf(format string, v ...interface{}) {
	// stdout carries the MCP protocol; diagnostics must go to stderr. The
	// engine already prefixes messages with "  WARN:" — pass through as-is.
	fmt.Fprintf(os.Stderr, format, v...)
}
