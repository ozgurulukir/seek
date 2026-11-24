package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
	"github.com/ozgurulukir/seek/internal/store/storetest"
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
	if err := db.UpsertFTSContext(context.Background(), 1, "Go Notes", "Go concurrency with channels and goroutines."); err != nil {
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

// captureStdoutInCmd runs fn while capturing everything it writes to stdout.
// Kept local to the package-cmd test files (cmd_test has its own copy).
func captureStdoutInCmd(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

// TestSearchWireParity_CLIVsMCP_ContextExpansion drives the real
// `seek search --json -C 1` path and the MCP seek_search tool against the same
// seeded index and asserts identical, actually-expanded wire results. The old
// parity test used Context=0, which could not catch a missing expandContext
// call on the CLI JSON path.
func TestSearchWireParity_CLIVsMCP_ContextExpansion(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	seed, err := store.Open(dbPath)
	if err != nil {
		storetest.SkipIfFTS5Disabled(t, err)
		t.Fatal(err)
	}
	col, err := seed.CreateCollection("notes", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := seed.UpsertDocument(col.ID, "/tmp/go.md", "Go Notes", "h1", 30, 3)
	if err != nil {
		t.Fatal(err)
	}
	chunks := []struct {
		seq        int
		content    string
		start, end int
	}{
		{0, "alpha intro about concurrency.", 1, 10},
		{1, "Go concurrency with channels and goroutines.", 11, 20},
		{2, "omega outro about concurrency.", 21, 30},
	}
	for _, ch := range chunks {
		if err := seed.InsertChunkWithLines(docID, ch.seq, ch.content, ch.start, ch.end, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := seed.UpsertFTSContext(context.Background(), docID, "Go Notes", "Go concurrency with channels and goroutines."); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := &config.AppConfig{DBPath: dbPath, CacheDir: filepath.Join(dir, "cache")}

	// MCP side: context=1 widens each hit with its neighbours.
	mcpDB, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	server, err := buildMCPServer(mcpDB, cfg)
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
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "seek_search",
		Arguments: map[string]any{"query": "concurrency", "limit": 5, "context": 1},
	})
	if err != nil {
		t.Fatalf("seek_search: %v", err)
	}
	session.Close()
	mcpDB.Close()
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("seek_search content not text: %T", res.Content[0])
	}

	// CLI side: the real `seek search --json -C 1` code path.
	var cliErr error
	stdout := captureStdoutInCmd(t, func() {
		cliErr = (&SearchCmd{Query: "concurrency", Limit: 5, JSON: true, Context: 1}).Run(cfg)
	})
	if cliErr != nil {
		t.Fatalf("SearchCmd.Run --json: %v", cliErr)
	}
	var output struct {
		Results []search.SearchResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("invalid CLI JSON: %v: %s", err, stdout)
	}
	if len(output.Results) == 0 {
		t.Fatal("seed produced no results; parity assertion is vacuous")
	}
	cliResults, err := json.Marshal(output.Results)
	if err != nil {
		t.Fatal(err)
	}
	if string(cliResults) != tc.Text {
		t.Errorf("wire parity broken with context=1:\nCLI: %s\nMCP: %s", cliResults, tc.Text)
	}

	// Expansion must be real: the FTS hit starts at seq 0, so radius 1 merges
	// chunks 0 and 1 into one span (lines 1-20, both chunk texts present).
	got := output.Results[0]
	if got.StartLine != 1 || got.EndLine != 20 {
		t.Errorf("expanded span = %d-%d, want 1-20", got.StartLine, got.EndLine)
	}
	if !strings.Contains(got.Content, "alpha intro") || !strings.Contains(got.Content, "Go concurrency") {
		t.Errorf("expanded content missing neighbouring chunks: %q", got.Content)
	}
}
