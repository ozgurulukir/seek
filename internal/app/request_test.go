package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

func newRequestTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	cfg := &config.AppConfig{
		Config: config.Config{VectorIndex: config.VectorIndexConfig{Backend: "linear"}},
		DBPath: filepath.Join(t.TempDir(), "seek.db"),
	}
	runtime, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime
}

// TestPlanSearchFieldParsing exercises --field name:value parsing end-to-end
// through the shared planner: valid fields add a FastFieldFilter, unknown or
// malformed values return an error.
func TestPlanSearchFieldParsing(t *testing.T) {
	runtime := newRequestTestRuntime(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		fields  []string
		wantErr string // "" = expect success
		wantN   int    // filters when successful
	}{
		{"topics", []string{"topics:concurrency"}, "", 1},
		{"entities with colon", []string{"entities:ORG:OpenAI"}, "", 1}, // value may contain ':'
		{"language", []string{"language:en"}, "", 1},
		{"multiple fields", []string{"topics:go", "language:en", "repo:x"}, "", 3},
		{"code lang", []string{"lang:rust"}, "", 1},
		{"unknown curated-field", []string{"title:foo"}, "unknown fast field", 0},
		{"unknown name", []string{"nonexistent:x"}, "unknown fast field", 0},
		{"empty value", []string{"topics:"}, "name:value", 0},
		{"no colon", []string{"topics"}, "name:value", 0},
		{"empty field", []string{":bar"}, "name:value", 0},
		{"whitespace not trimmed", []string{"topics :go"}, "unknown fast field", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := runtime.planSearch(ctx, SearchRequest{Fields: tc.fields})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("planSearch(%v) unexpected error: %v", tc.fields, err)
				}
				if got := len(opts.Filters.Items()); got != tc.wantN {
					t.Fatalf("filters = %d, want %d", got, tc.wantN)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("planSearch(%v) err = %v, want containing %q", tc.fields, err, tc.wantErr)
			}
		})
	}
}

func TestPlanSearchDynamicFieldAccepted(t *testing.T) {
	runtime := newRequestTestRuntime(t)
	ctx := context.Background()

	col, err := runtime.Store.CreateCollection("notes", store.CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := runtime.Store.UpsertDocument(col.ID, "/tmp/note.md", "Note", "h", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Store.FastFields().Set(docID, "author", "jane"); err != nil {
		t.Fatal(err)
	}

	// A field written by a producer without a curated entry filters fine.
	opts, err := runtime.planSearch(ctx, SearchRequest{Fields: []string{"author:jane"}})
	if err != nil {
		t.Fatalf("dynamic field rejected: %v", err)
	}
	if got := len(opts.Filters.Items()); got != 1 {
		t.Fatalf("filters = %d, want 1", got)
	}
}

func TestPlanSearchDefaults(t *testing.T) {
	runtime := newRequestTestRuntime(t)
	ctx := context.Background()

	opts, err := runtime.planSearch(ctx, SearchRequest{SortBy: "created_at"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.SortOrder != "desc" {
		t.Errorf("sort order default = %q, want desc", opts.SortOrder)
	}
	if opts.RRFK != search.DefaultRRFK {
		t.Errorf("RRFK = %d, want default %d", opts.RRFK, search.DefaultRRFK)
	}
	if opts.Analyzer == nil {
		t.Error("analyzer should be built when query mode is not raw")
	}
	if opts.QueryMode != "" {
		t.Errorf("query mode = %q, want unset config value", opts.QueryMode)
	}

	// Explicit sort order is preserved.
	opts, err = runtime.planSearch(ctx, SearchRequest{SortBy: "title", SortOrder: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.SortOrder != "asc" {
		t.Errorf("explicit sort order lost: %q", opts.SortOrder)
	}
}

func TestPlanSearchQueryModeRawSkipsAnalyzer(t *testing.T) {
	runtime := newRequestTestRuntime(t)
	runtime.cfgValue.Config.Search.QueryMode = "raw"
	ctx := context.Background()

	// Config raw (no flag): the engine must fully skip parsing.
	opts, err := runtime.planSearch(ctx, SearchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Analyzer != nil || opts.QueryMode != "raw" {
		t.Errorf("config raw: analyzer=%v mode=%q, want nil/raw", opts.Analyzer, opts.QueryMode)
	}

	// Flag parsed overrides config raw.
	opts, err = runtime.planSearch(ctx, SearchRequest{QueryMode: "parsed"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Analyzer == nil || opts.QueryMode != "parsed" {
		t.Errorf("flag override: analyzer=%v mode=%q, want analyzer/parsed", opts.Analyzer, opts.QueryMode)
	}

	// Flag raw overrides config parsed.
	runtime.cfgValue.Config.Search.QueryMode = "parsed"
	opts, err = runtime.planSearch(ctx, SearchRequest{QueryMode: "raw"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Analyzer != nil || opts.QueryMode != "raw" {
		t.Errorf("flag raw: analyzer=%v mode=%q, want nil/raw", opts.Analyzer, opts.QueryMode)
	}
}

func TestRunSearchVecRequiresClient(t *testing.T) {
	runtime := newRequestTestRuntime(t)
	runtime.EmbedClient = nil
	runtime.VLClient = nil

	_, err := runtime.RunSearch(context.Background(), SearchRequest{Query: "x", Mode: ModeVec})
	if err == nil || !strings.Contains(err.Error(), "vector search requires embedding API key") {
		t.Fatalf("vec without clients err = %v, want the API key message", err)
	}
}

func TestRunAggsEmptyIsNil(t *testing.T) {
	runtime := newRequestTestRuntime(t)
	aggs, err := runtime.RunAggs(context.Background(), SearchRequest{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if aggs != nil {
		t.Errorf("RunAggs with no specs = %v, want nil", aggs)
	}
}

func TestRunAggsRunsSpecs(t *testing.T) {
	runtime := newRequestTestRuntime(t)
	ctx := context.Background()
	col, err := runtime.Store.CreateCollection("notes", store.CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.md", "b.md"} {
		docID, err := runtime.Store.UpsertDocument(col.ID, "/tmp/"+name, name, "h", 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.Store.UpsertFTS(docID, name, "aggregation body text"); err != nil {
			t.Fatal(err)
		}
	}

	aggs, err := runtime.RunAggs(ctx, SearchRequest{Aggs: []string{"type:terms"}})
	if err != nil {
		t.Fatalf("RunAggs: %v", err)
	}
	buckets := aggs["type:terms"]
	if len(buckets) == 0 || buckets[0].Key != "markdown" || buckets[0].Count != 2 {
		t.Errorf("terms buckets = %+v, want markdown=2", buckets)
	}
}

func TestValidateFastField(t *testing.T) {
	runtime := newRequestTestRuntime(t)
	ctx := context.Background()

	if _, err := ValidateFastField(ctx, runtime.Store, "topics"); err != nil {
		t.Errorf("curated field rejected: %v", err)
	}
	if _, err := ValidateFastField(ctx, runtime.Store, "  LANG "); err != nil {
		t.Errorf("normalization failed: %v", err)
	}
	if _, err := ValidateFastField(ctx, runtime.Store, "nope"); err == nil {
		t.Error("unknown field must error")
	}

	// nil store degrades to curated-only validation.
	if _, err := ValidateFastField(ctx, nil, "topics"); err != nil {
		t.Errorf("nil-store curated check failed: %v", err)
	}
	if _, err := ValidateFastField(ctx, nil, "nope"); err == nil {
		t.Error("nil-store unknown field must error")
	}
}
