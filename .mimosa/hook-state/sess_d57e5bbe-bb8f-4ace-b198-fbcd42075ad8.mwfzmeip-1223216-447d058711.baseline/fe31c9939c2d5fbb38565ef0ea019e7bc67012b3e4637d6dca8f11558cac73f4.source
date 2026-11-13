package cmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
)

// TestSearchWireParity_CLIVsMCP is the keystone contract test: the CLI
// --json result mapping and the MCP seek_search payload must be byte
// identical for the same seed and request, because both go through
// search.NewSearchResults.
func TestSearchWireParity_CLIVsMCP(t *testing.T) {
	db := newMCPTestStore(t)
	col, err := db.CreateCollection("notes", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if _, err := db.UpsertDocument(col.ID, "/tmp/go.md", "Go Notes", "h1", 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertChunk(1, 0, "Go concurrency with channels and goroutines.", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertFTS(1, "Go Notes", "Go concurrency with channels and goroutines."); err != nil {
		t.Fatal(err)
	}

	cfg := &config.AppConfig{}
	server, err := buildMCPServer(db, cfg)
	if err != nil {
		t.Fatalf("buildMCPServer: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	go server.Run(ctx, st)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "seek_search",
		Arguments: map[string]any{"query": "concurrency", "limit": 5},
	})
	if err != nil {
		t.Fatalf("seek_search: %v", err)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("seek_search content not text: %T", res.Content[0])
	}

	// CLI side: same request through the shared runtime planner and the
	// shared wire mapping (the code path behind seek search --json).
	engine, err := app.NewSearchEngine(db, cfg)
	if err != nil {
		t.Fatalf("NewSearchEngine: %v", err)
	}
	runtime := &app.Runtime{Store: db, Search: engine}
	args := &mcpSearchArgs{Query: "concurrency", Limit: 5}
	results, err := runtime.RunSearch(ctx, args.searchRequest())
	if err != nil {
		t.Fatalf("RunSearch: %v", err)
	}
	if err := runtime.Search.EnrichContent(ctx, results); err != nil {
		t.Fatalf("EnrichContent: %v", err)
	}
	cliResults, err := json.Marshal(search.NewSearchOutput(args.Query, results, nil).Results)
	if err != nil {
		t.Fatal(err)
	}

	if len(cliResults) == 0 || string(cliResults) == "[]" {
		t.Fatal("seed produced no results; parity assertion is vacuous")
	}
	if string(cliResults) != tc.Text {
		t.Errorf("wire parity broken:\nCLI: %s\nMCP: %s", cliResults, tc.Text)
	}
}

// TestFieldsParity_CLIVsMCP pins seek_fields output to `seek fields --json`.
func TestFieldsParity_CLIVsMCP(t *testing.T) {
	db := newMCPTestStore(t)
	col, err := db.CreateCollection("notes", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	docID, err := db.UpsertDocument(col.ID, "/tmp/note.md", "Note", "h", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FastFields().Set(docID, "tags", "go,rust"); err != nil {
		t.Fatal(err)
	}

	cfg := &config.AppConfig{}
	server, err := buildMCPServer(db, cfg)
	if err != nil {
		t.Fatalf("buildMCPServer: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	go server.Run(ctx, st)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "seek_fields",
		Arguments: map[string]any{"field": "tags"},
	})
	if err != nil {
		t.Fatalf("seek_fields: %v", err)
	}
	tc := res.Content[0].(*mcp.TextContent)

	cliValues, err := listFieldValues(ctx, db, "tags", "", "", 50)
	if err != nil {
		t.Fatalf("listFieldValues: %v", err)
	}
	cliPayload, err := json.Marshal(cliValues)
	if err != nil {
		t.Fatal(err)
	}
	if string(cliPayload) != tc.Text {
		t.Errorf("fields parity broken:\nCLI: %s\nMCP: %s", cliPayload, tc.Text)
	}
}
