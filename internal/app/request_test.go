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
	return newRequestTestRuntimeWithConfig(t, config.Config{})
}

// newRequestTestRuntimeWithConfig opens a runtime whose config carries the
// given overrides on top of a linear vector index. Sections not overridden
// stay zero-value, mirroring hand-built configs in tests; production configs
// get their enabled flags defaulted by Load.
func newRequestTestRuntimeWithConfig(t *testing.T, overrides config.Config) *Runtime {
	t.Helper()
	cfg := &config.AppConfig{
		Config: config.Config{VectorIndex: config.VectorIndexConfig{Backend: "linear"}},
		DBPath: filepath.Join(t.TempDir(), "seek.db"),
	}
	cfg.Config.Search = overrides.Search
	cfg.Config.Filters = overrides.Filters
	cfg.Config.Aggregations = overrides.Aggregations
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
	runtime := newRequestTestRuntimeWithConfig(t, config.Config{
		Aggregations: config.AggregationConfig{Enabled: true},
	})
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

// TestRunSearchUsesConfigDefaultLimit pins the search.default_limit wiring:
// an unset limit (-l absent / MCP limit absent) resolves to the config value,
// an explicit limit still wins, and no config means the engine default.
func TestRunSearchUsesConfigDefaultLimit(t *testing.T) {
	runtime := newRequestTestRuntimeWithConfig(t, config.Config{
		Search: config.SearchConfig{DefaultLimit: 1},
	})
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

	// Unset limit → config default (1).
	results, err := runtime.RunSearch(ctx, SearchRequest{Query: "aggregation", Mode: ModeLex})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("unset limit returned %d results, want 1 (search.default_limit)", len(results))
	}

	// Explicit limit wins over the config default.
	results, err = runtime.RunSearch(ctx, SearchRequest{Query: "aggregation", Mode: ModeLex, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("explicit limit returned %d results, want 2", len(results))
	}
}

// TestFiltersDefaultCollection pins the filters.default_collection wiring:
// it applies only when the request carries no collection/repo filter, and
// filters.enabled gates it.
func TestFiltersDefaultCollection(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		filters config.FilterConfig
		reqCol  string
		want    int
	}{
		{"default collection applies", config.FilterConfig{Enabled: true, DefaultCollection: "notes"}, "", 1},
		{"explicit collection wins", config.FilterConfig{Enabled: true, DefaultCollection: "notes"}, "other", 1},
		{"disabled ignores default", config.FilterConfig{Enabled: false, DefaultCollection: "notes"}, "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := newRequestTestRuntimeWithConfig(t, config.Config{Filters: tc.filters})
			for _, colName := range []string{"notes", "other"} {
				col, err := runtime.Store.CreateCollection(colName, store.CollectionTypeMarkdown, "/tmp", "**/*.md")
				if err != nil {
					t.Fatal(err)
				}
				docID, err := runtime.Store.UpsertDocument(col.ID, "/tmp/"+colName+".md", colName+".md", "h", 1, 1)
				if err != nil {
					t.Fatal(err)
				}
				if err := runtime.Store.UpsertFTS(docID, colName+".md", "aggregation body text"); err != nil {
					t.Fatal(err)
				}
			}

			results, err := runtime.RunSearch(ctx, SearchRequest{Query: "aggregation", Mode: ModeLex, Collection: tc.reqCol})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != tc.want {
				t.Errorf("got %d results, want %d", len(results), tc.want)
			}
		})
	}
}

// TestRunAggsDisabledErrors pins the aggregations.enabled gate.
func TestRunAggsDisabledErrors(t *testing.T) {
	runtime := newRequestTestRuntimeWithConfig(t, config.Config{
		Aggregations: config.AggregationConfig{Enabled: false},
	})
	_, err := runtime.RunAggs(context.Background(), SearchRequest{Aggs: []string{"type:terms"}})
	if err == nil || !strings.Contains(err.Error(), "aggregations are disabled") {
		t.Fatalf("RunAggs with aggregations disabled err = %v, want the disabled message", err)
	}
}

// TestRunSearchParsedModeMatchesNonPrefixStems pins issue #98 end-to-end: the
// unstemmed FTS5 index stores surface tokens, so in the default (parsed) query
// mode a term whose Porter stem is not a prefix of the surface form must still
// match. `body` used to render as `bodi*` and silently return nothing.
func TestRunSearchParsedModeMatchesNonPrefixStems(t *testing.T) {
	runtime := newRequestTestRuntime(t)
	ctx := context.Background()

	const body = "# Doc\n\nbody of the study and the city\n"
	col, err := runtime.Store.CreateCollection("notes", store.CollectionTypeMarkdown, "/tmp", "**/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docID, err := runtime.Store.UpsertDocument(col.ID, "/tmp/n.md", "Doc", "# Doc", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Store.UpsertFTS(docID, "Doc", body); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"body", "study", "city"} {
		results, err := runtime.RunSearch(ctx, SearchRequest{Query: q, Mode: ModeLex})
		if err != nil {
			t.Fatalf("RunSearch(%q): %v", q, err)
		}
		if len(results) == 0 {
			t.Errorf("RunSearch(%q) in default query mode returned no results, want the document", q)
		}
	}
}

func TestPlanSearchSortByValidation(t *testing.T) {
	runtime := newRequestTestRuntime(t)
	ctx := context.Background()

	// Registry sort fields (documents columns and curated fast fields) pass.
	for _, field := range []string{"created_at", "line_count", "title", "lang"} {
		if _, err := runtime.planSearch(ctx, SearchRequest{SortBy: field}); err != nil {
			t.Errorf("planSearch(sort-by %s) unexpected error: %v", field, err)
		}
	}

	// Unknown names error instead of silently degrading to relevance order.
	_, err := runtime.planSearch(ctx, SearchRequest{SortBy: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "--sort-by") {
		t.Fatalf("planSearch(sort-by bogus) err = %v, want --sort-by unknown-field error", err)
	}
}
