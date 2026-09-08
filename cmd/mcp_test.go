package cmd

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

func newMCPTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags fts5 ./... or make test")
		}
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newMCPTestServer(t *testing.T) (*mcp.Server, *mcp.Client) {
	t.Helper()
	db := newMCPTestStore(t)
	cfg := &config.AppConfig{}
	server, err := buildMCPServer(db, cfg)
	if err != nil {
		t.Fatalf("buildMCPServer: %v", err)
	}
	return server, mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
}

func TestMCPServer_Roundtrip(t *testing.T) {
	server, client := newMCPTestServer(t)
	ctx := context.Background()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go server.Run(ctx, serverTransport)

	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// tools/list
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range tools.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"seek_search", "seek_status", "seek_autocomplete"} {
		if !got[want] {
			t.Errorf("missing tool %q; have %v", want, tools)
		}
	}

	// seek_search on an empty index returns an empty JSON array, not an error.
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "seek_search",
		Arguments: map[string]any{"query": "anything", "limit": 3},
	})
	if err != nil {
		t.Fatalf("seek_search: %v", err)
	}
	if res.IsError {
		t.Fatalf("seek_search returned tool error: %v", res.Content)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("seek_search content not text: %T", res.Content[0])
	}
	var results []mcpSearchResult
	if err := json.Unmarshal([]byte(tc.Text), &results); err != nil {
		t.Fatalf("parse search payload: %v (%s)", err, tc.Text)
	}
	if len(results) != 0 {
		t.Errorf("empty index should return zero results, got %d", len(results))
	}

	// seek_status on an empty index returns an empty array.
	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "seek_status", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("seek_status: %v", err)
	}
	tc = res.Content[0].(*mcp.TextContent)
	var collections []map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &collections); err != nil {
		t.Fatalf("parse status payload: %v (%s)", err, tc.Text)
	}
	if len(collections) != 0 {
		t.Errorf("empty index should list zero collections, got %v", tc.Text)
	}

	// seek_autocomplete returns a suggestions envelope.
	res, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "seek_autocomplete", Arguments: map[string]any{"prefix": "x"},
	})
	if err != nil {
		t.Fatalf("seek_autocomplete: %v", err)
	}
	tc = res.Content[0].(*mcp.TextContent)
	var ac struct {
		Query       string   `json:"query"`
		Suggestions []string `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(tc.Text), &ac); err != nil {
		t.Fatalf("parse autocomplete payload: %v (%s)", err, tc.Text)
	}
	if ac.Query != "x" {
		t.Errorf("autocomplete query wrong: %+v", ac)
	}
	// Suggestions may be nil on an empty vocabulary; JSON must still be an
	// array (or absent), not an error — verify it parses as a list shape.
	if ac.Suggestions != nil && len(ac.Suggestions) > 0 && ac.Suggestions[0] == "" {
		t.Errorf("suggestions malformed: %+v", ac.Suggestions)
	}
}

func TestMCPSearchResult_Shape(t *testing.T) {
	// The MCP result must mirror `seek search --json` field names.
	b, err := json.Marshal(mcpSearchResult{ChunkID: 1, DocumentID: 2, Title: "t", ContentKind: "full", Score: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"chunk_id", "document_id", "title", "content", "content_kind", "score", "chunk_type", "start_line", "end_line"} {
		if _, ok := m[key]; !ok {
			t.Errorf("mcpSearchResult missing %q key: %s", key, b)
		}
	}
}

func TestMCPContentKind(t *testing.T) {
	if got := mcpContentKind(store.SearchResult{ChunkID: 5}, &mcpSearchResult{ChunkID: 5}); got != "full" {
		t.Errorf("chunk-level = %q, want full", got)
	}
	if got := mcpContentKind(store.SearchResult{ChunkID: 0}, &mcpSearchResult{}); got != "snippet" {
		t.Errorf("document-level = %q, want snippet", got)
	}
}
