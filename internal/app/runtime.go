// Package app owns the process-level composition root for seek commands.
// Commands receive one Runtime so Store, vector index, search, and indexer
// share resource ownership and close order.
package app

import (
	"context"
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
		_ = s.Close()
		return nil, err
	}

	backend := strings.ToLower(strings.TrimSpace(cfg.Config.VectorIndex.Backend))
	if backend == "" || backend == "hnsw" || backend == "linear" {
		vectorIndex, err := store.NewVectorIndex(cfg)
		if err != nil {
			return closeOnError(fmt.Errorf("open vector index: %w", err))
		}
		s.SetVectorIndex(vectorIndex)
		if backend != "linear" {
			if err := s.RecoverVectorIndex(context.Background()); err != nil {
				return closeOnError(fmt.Errorf("recover vector index: %w", err))
			}
		}
		r.VectorIndexEnabled = true
		if warning, ok := vectorIndex.(store.VectorIndexWarning); ok && warning.Warning() != "" {
			r.Warnings = append(r.Warnings, warning.Warning())
		}
	} else if backend != "linear" {
		return closeOnError(fmt.Errorf("unsupported vector index backend %q", backend))
	}

	r.EmbedClient = embed.NewClientFromConfig(cfg)
	r.VLClient = embed.NewVLClientFromConfig(cfg)
	if r.VLClient != nil {
		r.Search = search.NewEngineWithVL(NewStoreSearchRepository(s), r.EmbedClient, r.VLClient)
	} else {
		r.Search = search.NewEngine(NewStoreSearchRepository(s), r.EmbedClient)
	}
	r.Indexer = indexer.NewWithDependencies(cfg, s, indexer.NewConfigExtractorResolver(cfg), s)
	return r, nil
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
	return pipeline.EmbedPendingContext(ctx, r.config(), r.Store, opts, log)
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
