package cmd

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

func newMCPTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		if strings.Contains(err.Error(), "SQLite FTS5 not enabled") {
			t.Skip("SQLite FTS5 not enabled. Run tests with: go test -tags \"fts5 sqlite_fts5\" ./... or make test")
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
	for _, want := range []string{"seek_search", "seek_fields", "seek_status", "seek_autocomplete"} {
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
	var results []search.SearchResult
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
	// The MCP result is the shared wire struct; pin the field names both
	// surfaces emit.
	b, err := json.Marshal(search.SearchResult{ChunkID: 1, DocumentID: 2, Title: "t", ContentKind: "full", Score: 0.5})
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

func TestMCPSearch_CollectionFilter(t *testing.T) {
	db := newMCPTestStore(t)
	col, err := db.CreateCollection("zigcol", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if _, err := db.UpsertDocument(col.ID, "/tmp/zig.md", "Zig Patterns", "h", 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertChunk(1, 0, "Zig comptime is powerful and the language is simple.", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertFTS(1, "Zig Patterns", "Zig comptime is powerful and the language is simple."); err != nil {
		t.Fatal(err)
	}
	col2, err := db.CreateCollection("rustcol", "markdown", "/tmp", "**/*.md")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if _, err := db.UpsertDocument(col2.ID, "/tmp/rust.md", "Rust Patterns", "h2", 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertChunk(2, 0, "Rust borrow checker rules.", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertFTS(2, "Rust Patterns", "Rust borrow checker rules."); err != nil {
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
		Arguments: map[string]any{"query": "patterns", "limit": 5, "collection": "zigcol"},
	})
	if err != nil {
		t.Fatalf("seek_search: %v", err)
	}
	tc := res.Content[0].(*mcp.TextContent)
	var results []search.SearchResult
	if err := json.Unmarshal([]byte(tc.Text), &results); err != nil {
		t.Fatalf("parse: %v (%s)", err, tc.Text)
	}
	for _, r := range results {
		if r.Collection != "zigcol" {
			t.Errorf("collection filter leaked %q result", r.Collection)
		}
	}
	if len(results) == 0 {
		t.Error("filtered search returned no results")
	}
}

func TestMCPSearch_AgentArguments(t *testing.T) {
	db := newMCPTestStore(t)
	col, err := db.CreateCollection("zay", "code", t.TempDir(), "**/*.zig")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := db.UpsertDocument(col.ID, "/tmp/test.zig", "test", "hash", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FastFields().Set(doc, "lang", "zig"); err != nil {
		t.Fatal(err)
	}
	if err := db.FastFields().Set(doc, "repo", "zay"); err != nil {
		t.Fatal(err)
	}
	content := "RefusalBlock ContentBlock refusal output_text"
	if err := db.InsertChunk(doc, 0, content, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertFTS(doc, "test", content); err != nil {
		t.Fatal(err)
	}
	if err := db.FastFields().Set(doc, "doc_type", "code"); err != nil {
		t.Fatal(err)
	}
	server, err := buildMCPServer(db, &config.AppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	go server.Run(ctx, st)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, tc := range []struct {
		name        string
		field, aggs any
		wantError   bool
	}{
		{"empty strings", "", "", false},
		{"null", nil, nil, false},
		{"empty arrays", []string{}, []string{}, false},
		{"field array", []string{"lang:zig"}, nil, false},
		{"aggregation array", nil, []string{"doc_type:terms"}, false},
		{"field string", "lang:zig", nil, true},
		{"aggregation string", nil, "doc_type:terms", true},
		{"invalid element", []int{1}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "seek_search", Arguments: map[string]any{
				"query": "RefusalBlock ContentBlock refusal output_text", "limit": 20,
				"lex": true, "vec": false, "collection": "zay", "repo": "zay",
				"doc_type": "code", "lang": "zig", "tag": "", "after": "", "before": "",
				"chunk_type": "text", "path": "", "workspace": "", "context": 1,
				"field": tc.field, "aggs": tc.aggs, "sort_by": "_score", "sort_order": "desc",
			}})
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError != tc.wantError {
				t.Fatalf("IsError = %v, want %v: %+v", res.IsError, tc.wantError, res.Content[0].(*mcp.TextContent).Text)
			}
			if !tc.wantError {
				var results []search.SearchResult
				if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &results); err != nil {
					t.Fatal(err)
				}
				if len(results) != 1 || results[0].DocumentID != doc {
					t.Fatalf("expected matching document %d, got %+v", doc, results)
				}
			}

		})
	}
	t.Run("unsupported score order", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "seek_search", Arguments: map[string]any{
			"query": "refusal", "lex": true, "sort_by": "_score", "sort_order": "asc",
		}})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "sort_order with _score") {
			t.Fatalf("expected explicit score order error: %+v", res)
		}
	})

}
