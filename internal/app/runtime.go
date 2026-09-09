// Package app owns the process-level composition root for seek commands.
// Commands receive one Runtime so Store, vector index, search, and indexer
// share resource ownership and close order.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/pipeline"
	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

// Runtime contains the process-owned services for a command invocation.
type Runtime struct {
	Store              *store.Store
	Indexer            *indexer.Indexer
	Pipeline           *pipeline.Pipeline
	Search             *search.Engine
	EmbedClient        *embed.Client
	VLClient           *embed.VLClient
	VectorIndexEnabled bool
	Warnings           []string
	cfgValue           *config.AppConfig
}

// Open builds the complete runtime graph and validates configured vector
// persistence before a command starts doing work. A configured HNSW failure
// is returned; a recoverable corrupt/legacy file is surfaced in Warnings by
// NewVectorIndex while a fresh graph is used.
func Open(cfg *config.AppConfig) (*Runtime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("open runtime: nil config")
	}
	s, err := OpenStore(cfg)
	if err != nil {
		return nil, err
	}
	r := &Runtime{Store: s, cfgValue: cfg}
	closeOnError := func(err error) (*Runtime, error) {
		return nil, errors.Join(err, s.Close())
	}

	warnings, enabled, err := ConfigureVectorIndex(context.Background(), s, cfg)
	if err != nil {
		return closeOnError(err)
	}
	r.Warnings = append(r.Warnings, warnings...)
	r.VectorIndexEnabled = enabled

	provider, err := embed.NewProviderFromConfig(cfg)
	if err != nil {
		return closeOnError(fmt.Errorf("build embedding provider: %w", err))
	}
	r.Indexer = indexer.NewWithDependencies(cfg, s, indexer.NewConfigExtractorResolver(cfg), s)
	r.EmbedClient, _ = provider.Document.(*embed.Client)
	r.VLClient, _ = provider.VLQuery.(*embed.VLClient)
	r.Search = search.NewEngineWithProvider(NewStoreSearchRepository(s), provider)
	r.Pipeline = pipeline.New(cfg, s, r.Indexer, provider)
	return r, nil
}

// ConfigureVectorIndex is the single vector composition seam used by the
// full runtime and compatibility/test entrypoints that already own a Store.
func ConfigureVectorIndex(ctx context.Context, s *store.Store, cfg *config.AppConfig) ([]string, bool, error) {
	if s == nil || cfg == nil {
		return nil, false, fmt.Errorf("configure vector index: nil dependency")
	}
	backend := strings.ToLower(strings.TrimSpace(cfg.Config.VectorIndex.Backend))
	if backend != "" && backend != "hnsw" && backend != "linear" {
		return nil, false, fmt.Errorf("unsupported vector index backend %q", cfg.Config.VectorIndex.Backend)
	}
	vectorIndex, err := store.NewVectorIndex(cfg)
	if err != nil {
		return nil, false, fmt.Errorf("open vector index: %w", err)
	}
	s.SetVectorIndex(vectorIndex)
	if backend != "linear" {
		if err := s.RecoverVectorIndex(ctx); err != nil {
			return nil, false, fmt.Errorf("recover vector index: %w", err)
		}
	}
	var warnings []string
	if warning, ok := vectorIndex.(store.VectorIndexWarning); ok && warning.Warning() != "" {
		warnings = append(warnings, warning.Warning())
	}
	return warnings, true, nil
}

// NewSearchEngine constructs the search module from runtime-owned provider
// capabilities. Commands and MCP compatibility helpers do not build clients.
func NewSearchEngine(s *store.Store, cfg *config.AppConfig) (*search.Engine, error) {
	provider, err := embed.NewProviderFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build embedding provider: %w", err)
	}
	return search.NewEngineWithProvider(NewStoreSearchRepository(s), provider), nil
}

// OpenStore is the lightweight composition path for commands that only need
// SQLite persistence (status, add, remove, and parser management). It shares
// the same compression normalization as the full runtime without creating
// unused network clients or a vector index.
func OpenStore(cfg *config.AppConfig) (*store.Store, error) {
	if cfg == nil {
		return nil, fmt.Errorf("open store: nil config")
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	s.ConfigureCompression(cfg.Config.Compression)
	return s, nil
}

// EmbedPending runs embedding against the same Store used by indexing.
func (r *Runtime) EmbedPending(ctx context.Context, opts pipeline.Options, log pipeline.Logger) error {
	if r == nil || r.Store == nil {
		return fmt.Errorf("embed runtime: store is nil")
	}
	opts.VectorIndex = opts.VectorIndex && r.VectorIndexEnabled
	if r.Pipeline == nil {
		return fmt.Errorf("embed runtime: pipeline is nil")
	}
	return r.Pipeline.EmbedPendingContext(ctx, opts, log)
}

func (r *Runtime) config() *config.AppConfig {
	if r == nil {
		return nil
	}
	return r.cfgValue
}

// Close flushes persistent vectors before closing SQLite. Store.Close owns
// the ordering and joins both errors.
func (r *Runtime) Close() error {
	if r == nil || r.Store == nil {
		return nil
	}
	return r.Store.Close()
}
