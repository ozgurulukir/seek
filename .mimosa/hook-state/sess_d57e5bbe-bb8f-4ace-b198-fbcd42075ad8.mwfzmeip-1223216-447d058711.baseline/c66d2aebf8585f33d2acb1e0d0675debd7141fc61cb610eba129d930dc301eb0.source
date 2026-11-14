package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

// ErrVectorUnavailable lets adapters report capability failures without parsing error text.
var ErrVectorUnavailable = errors.New("vector search requires embedding API key")

// SearchRequest is the surface-neutral description of one search call. The
// CLI (flags) and MCP (tool arguments) adapters both build it, and Runtime
// turns it into engine calls — request policy lives here once instead of
// being mirrored across the two surfaces.
//
// Zero-value fields mean "unset"; config fallbacks are applied by the
// planner. Rendering-only concerns (--json, terminal output, snippets) stay
// in the adapters.
type SearchRequest struct {
	Query string
	Limit int // <=0 falls back to search.default_limit, then the engine default
	Mode  SearchMode

	// Filter slots. Collection and Repo: Repo is an alias that also filters
	// by collection when Collection is empty (historical CLI semantics);
	// when both are set both filters apply.
	Collection string
	Repo       string
	DocType    string
	Lang       string
	Tag        string
	After      string // RFC3339
	Before     string // RFC3339
	ChunkType  string // "text" (default) or "image"
	Path       string // GLOB
	Workspace  string
	Fields     []string // each "name:value"; the value may contain ':'

	SortBy    string
	SortOrder string // "" with SortBy set resolves to "desc"

	QueryMode   string // flag override; "" falls back to config search.query_mode
	AnalyzeLang string // flag override; "" falls back to config, then the default
	Aggs        []string
}

// SearchMode selects the retrieval legs. ModeAuto is the BM25+vector hybrid.
type SearchMode int

const (
	ModeAuto SearchMode = iota
	ModeLex
	ModeVec
)

// effectiveQueryMode resolves flag-over-config: a non-empty flag wins, else
// the configured search.query_mode.
func effectiveQueryMode(flagMode, cfgMode string) string {
	if flagMode != "" {
		return flagMode
	}
	return cfgMode
}

// EffectiveAnalyzeLang resolves the analysis language: an explicit language
// wins, then search.analyze_lang from config, then the built-in default.
// Shared by the search planner, `seek search --analyze`, and `seek analyze`.
func EffectiveAnalyzeLang(flagLang string, cfg *config.AppConfig) string {
	if flagLang != "" {
		return flagLang
	}
	if cfg != nil && cfg.Config.Search.AnalyzeLang != "" {
		return cfg.Config.Search.AnalyzeLang
	}
	return config.DefaultAnalyzeLang
}

// planSearch compiles req against the runtime configuration: builds the
// FilterSet (including "name:value" parsing and fast-field validation),
// resolves the effective query mode and analyze language, constructs the
// Analyzer unless the effective mode is "raw", and normalizes RRFK and the
// sort order.
func (r *Runtime) planSearch(ctx context.Context, req SearchRequest) (search.Options, error) {
	opts := search.Options{Limit: req.Limit}

	// One resolver snapshot (one DISTINCT query) serves every --field and
	// --sort-by occurrence in this request; it is built only when the request
	// actually carries field validation work. Without a store (degraded test
	// runtime) only curated names validate.
	known := store.FieldKnownFunc(store.ValidFastField)
	if len(req.Fields) > 0 || req.SortBy != "" {
		var err error
		known, err = store.NewFieldKnownFunc(ctx, r.Store)
		if err != nil {
			return opts, err
		}
	}

	filters, err := r.buildRequestFilters(known, req)
	if err != nil {
		return opts, err
	}
	opts.Filters = filters

	// Degraded runtimes (the MCP test seam) may carry a nil config; every
	// setting then falls back to its default.
	cfgMode, cfgRRFK := "", 0
	if cfg := r.config(); cfg != nil {
		cfgMode = cfg.Config.Search.QueryMode
		cfgRRFK = cfg.Config.Search.RRFK
	}

	opts.QueryMode = effectiveQueryMode(req.QueryMode, cfgMode)
	if opts.QueryMode != "raw" {
		opts.Analyzer = search.NewAnalyzer(EffectiveAnalyzeLang(req.AnalyzeLang, r.config()), true, true)
	}

	opts.RRFK = cfgRRFK
	if opts.RRFK <= 0 {
		opts.RRFK = search.DefaultRRFK
	}

	opts.SortBy = req.SortBy
	if opts.SortBy != "" {
		if err := validateSortField(known, opts.SortBy); err != nil {
			return opts, err
		}
	}
	opts.SortOrder = req.SortOrder
	if opts.SortBy != "" && opts.SortOrder == "" {
		opts.SortOrder = "desc"
	}
	return opts, nil
}

// validateSortField accepts registry sort fields (curated fast fields plus
// documents-column pseudo-fields) and any fast field physically present in
// the index, mirroring --field validation. Unknown names error instead of
// silently degrading to relevance order.
func validateSortField(known store.FieldKnownFunc, field string) error {
	if store.SortableField(field) {
		return nil
	}
	if !known(field) {
		return fmt.Errorf("--sort-by: unknown field %q (%s)", field, store.FieldDiscoveryHint())
	}
	return nil
}

// buildRequestFilters maps the request's filter slots into the domain
// FilterSet. A request with no filter set yields a nil FilterSet.
func (r *Runtime) buildRequestFilters(known store.FieldKnownFunc, req SearchRequest) (*search.FilterSet, error) {
	colName := req.Collection
	if colName == "" {
		colName = req.Repo
	}
	// filters.default_collection applies only when the request carries no
	// explicit collection/repo filter, and only when filtering is enabled.
	// An unknown name must error, not silently blind every search to zero
	// results (the collection subquery would resolve to NULL).
	if colName == "" {
		if cfg := r.config(); cfg != nil && cfg.Config.Filters.Enabled {
			colName = cfg.Config.Filters.DefaultCollection
			if colName != "" && r.Store != nil {
				if _, err := r.Store.GetCollectionByName(colName); err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						return nil, fmt.Errorf("filters.default_collection %q does not match any collection (see `seek collection list`)", colName)
					}
					return nil, fmt.Errorf("resolve filters.default_collection: %w", err)
				}
			}
		}
	}

	filters := search.NewFilterSet()
	if colName != "" {
		filters.Add(search.CollectionFilter(colName))
	}
	if req.DocType != "" {
		filters.Add(search.DocTypeFilter(req.DocType))
	}
	if req.Lang != "" {
		filters.Add(search.LanguageFilter(strings.ToLower(req.Lang)))
	}
	if req.Repo != "" {
		filters.Add(search.RepositoryFilter(req.Repo))
	}
	if req.Tag != "" {
		filters.Add(search.TagFilter(req.Tag))
	}
	if req.After != "" || req.Before != "" {
		filters.Add(search.DateRangeFilter(req.After, req.Before))
	}
	if req.ChunkType != "" {
		ct := 0
		if strings.ToLower(req.ChunkType) == "image" {
			ct = 1
		}
		filters.Add(search.ChunkTypeFilter(search.ChunkType(ct)))
	}
	if req.Path != "" {
		filters.Add(search.PathFilter(req.Path))
	}
	if req.Workspace != "" {
		filters.Add(search.WorkspaceFilter(req.Workspace))
	}
	if len(req.Fields) > 0 {
		// The per-request oracle accepts curated fast fields plus any field
		// physically present in the index; planSearch built it once above so
		// this loop, --sort-by, and one-off validation share the same rule.
		for _, f := range req.Fields {
			field, value, ok := strings.Cut(f, ":")
			if !ok || field == "" || value == "" {
				return nil, fmt.Errorf("--field must be 'name:value' (got %q)", f)
			}
			// Lowercase for registry lookup but never trim: a space in the
			// name means "unknown field" (the historical defense against
			// sloppy "field :value" input).
			field = strings.ToLower(field)
			if !known(field) {
				return nil, fmt.Errorf("--field: unknown fast field %q (%s)", field, store.FieldDiscoveryHint())
			}
			filters.Add(search.FastFieldFilter(field, value))
		}
	}
	if len(filters.Items()) == 0 {
		return nil, nil
	}
	return filters, nil
}

// RunSearch plans and executes req against the engine owned by the runtime.
func (r *Runtime) RunSearch(ctx context.Context, req SearchRequest) ([]search.Result, error) {
	if req.Limit <= 0 {
		// search.default_limit resolves the unset CLI -l flag / MCP limit
		// argument; the engine's DefaultLimit is the final fallback.
		if cfg := r.config(); cfg != nil && cfg.Config.Search.DefaultLimit > 0 {
			req.Limit = cfg.Config.Search.DefaultLimit
		}
	}
	opts, err := r.planSearch(ctx, req)
	if err != nil {
		return nil, err
	}
	engine := r.Search
	switch req.Mode {
	case ModeLex:
		return engine.SearchBM25(ctx, req.Query, req.Limit, opts)
	case ModeVec:
		if r.EmbedClient == nil && r.VLClient == nil {
			return nil, ErrVectorUnavailable
		}
		return engine.SearchVector(ctx, req.Query, req.Limit, opts)
	default:
		return engine.SearchHybrid(ctx, req.Query, req.Limit, opts)
	}
}

// RunAggs runs the request's aggregation specs with the request's compiled
// filters. Returns a nil map when no aggregations are requested (the JSON
// envelope omits "aggs" in that case).
func (r *Runtime) RunAggs(ctx context.Context, req SearchRequest) (map[string][]search.Bucket, error) {
	if len(req.Aggs) == 0 {
		return nil, nil
	}
	if cfg := r.config(); cfg != nil && !cfg.Config.Aggregations.Enabled {
		return nil, fmt.Errorf("aggregations are disabled (set aggregations.enabled: true in config.yaml)")
	}
	opts, err := r.planSearch(ctx, req)
	if err != nil {
		return nil, err
	}
	return r.Search.RunAggregations(ctx, req.Aggs, opts.Filters)
}
