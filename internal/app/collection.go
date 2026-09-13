package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/pipeline"
	"github.com/ozgurulukir/seek/internal/store"
)

// CollectionService is the collection lifecycle boundary between commands and
// persistence. Commands call this service instead of orchestrating SQL/Store
// details directly, so the CLI and MCP surfaces share one request path.
//
// Rename, Reindex, and Backfill never touch source files; they only manage
// the index lifecycle described by the plan (source files remain the source
// of truth).
type CollectionService struct {
	store    *store.Store
	pipeline *pipeline.Pipeline
	cfg      *config.AppConfig
	indexer  *indexer.Indexer
}

// NewCollectionService builds the lifecycle service from a composed Runtime.
func NewCollectionService(r *Runtime) *CollectionService {
	if r == nil {
		return nil
	}
	return &CollectionService{
		store:    r.Store,
		pipeline: r.Pipeline,
		cfg:      r.cfgValue,
		indexer:  r.Indexer,
	}
}

// NewCollectionServiceFromStore builds the lifecycle service for the
// READ-ONLY surface (List/Show) from a Store alone, matching the lightweight
// composition path `seek collection list/show` uses. They only need
// CollectionDetails, so they must not open the full runtime (embedding
// provider, vector index) — that would inherit its latency, warnings, and
// failure modes (review M3). Rename/Reindex/Backfill still require the full
// Runtime via NewCollectionService.
func NewCollectionServiceFromStore(s *store.Store) *CollectionService {
	if s == nil {
		return nil
	}
	return &CollectionService{store: s}
}

// CollectionInfo is the presentation contract for collection list/show.
// JSON tags keep `seek collection list --json` / `show --json` aligned with
// the snake_case wire convention used by `seek search --json` and
// `seek fields --json`.
type CollectionInfo struct {
	Name            string               `json:"name"`
	Type            store.CollectionType `json:"type"`
	Path            string               `json:"path"`
	Pattern         string               `json:"pattern,omitempty"`
	ParserName      string               `json:"parser_name,omitempty"`
	ParserVersion   int                  `json:"parser_version,omitempty"`
	Backend         string               `json:"backend,omitempty"`
	Documents       int                  `json:"documents"`
	Chunks          int                  `json:"chunks"`
	EmbeddedChunks  int                  `json:"embedded_chunks"`
	SemanticCurrent int                  `json:"semantic_current"`
	SemanticStale   int                  `json:"semantic_stale"`
	SemanticError   int                  `json:"semantic_error"`
	// SemanticNone is Documents minus the recorded current/stale/error states.
	SemanticNone int `json:"semantic_none"`
	// LastSyncErrors carries the most recent per-file sync failures. Sync
	// failures are not yet persisted to the store, so this stays empty until a
	// sync-error log lands; it is part of the surface now so show output does
	// not need a schema change later.
	LastSyncErrors []string `json:"last_sync_errors,omitempty"`
}

// List returns every collection with its detail accounting in one store query.
func (s *CollectionService) List(ctx context.Context) ([]CollectionInfo, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("collection service: store is nil")
	}
	details, err := s.store.CollectionDetails(ctx)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	out := make([]CollectionInfo, 0, len(details))
	for _, d := range details {
		out = append(out, toCollectionInfo(d))
	}
	return out, nil
}

// Show returns the detail accounting for one collection by name.
func (s *CollectionService) Show(ctx context.Context, name string) (CollectionInfo, error) {
	if s == nil || s.store == nil {
		return CollectionInfo{}, fmt.Errorf("collection service: store is nil")
	}
	details, err := s.store.CollectionDetails(ctx)
	if err != nil {
		return CollectionInfo{}, fmt.Errorf("show collection: %w", err)
	}
	for _, d := range details {
		if d.Name == name {
			return toCollectionInfo(d), nil
		}
	}
	return CollectionInfo{}, fmt.Errorf("collection %q not found", name)
}

// Rename atomically renames a collection. Only the index label changes; the
// source path and every document/chunk/FTS/fast-field/embedding stay intact.
func (s *CollectionService) Rename(ctx context.Context, oldName, newName string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("collection service: store is nil")
	}
	if strings.TrimSpace(newName) == "" {
		return fmt.Errorf("rename collection: new name must not be empty")
	}
	return s.store.RenameCollection(ctx, oldName, newName)
}

// ReindexOptions controls a full collection reindex.
type ReindexOptions struct {
	// AllowVectorSpaceChange permits clearing the embedding profile and
	// re-embedding the collection's chunks when the stored profile mismatches
	// the current config. Without it, a mismatch refuses with a reindex hint
	// so the vector space is never changed silently. The clear is collection-
	// scoped safe only when no OTHER collection holds embedded chunks (the
	// profile is store-global); otherwise the reindex refuses even with
	// AllowVectorSpaceChange set.
	AllowVectorSpaceChange bool
}

// ReindexResult is the outcome of one reindex pass.
type ReindexResult struct {
	// Report is the underlying collection sync outcome.
	Report indexer.SyncReport
	// VectorSpaceChanged reports whether the embedding profile was cleared
	// because the stored vector space did not match the current config.
	VectorSpaceChanged bool
}

// Reindex re-reads the source files and rebuilds the FTS/chunks/fast-fields
// lifecycle, then re-embeds the collection's chunks. It follows the embedding
// profile contract: before any re-embedding the stored profile is validated
// against the current config. A mismatch refuses the pass (unless
// AllowVectorSpaceChange opts into the clear/reclaim contract) instead of
// silently mixing two vector spaces, and refuses outright when other
// collections still hold embedded chunks — the profile is store-global, so a
// collection-scoped reindex can never safely clear it while they do. A config
// without an embedding model skips profile validation — there is nothing to
// embed, so a keyword-only reindex is never blocked.
func (s *CollectionService) Reindex(ctx context.Context, name string, opts ReindexOptions, log pipeline.Logger) (ReindexResult, error) {
	if s == nil || s.store == nil || s.pipeline == nil {
		return ReindexResult{}, fmt.Errorf("collection service: not configured")
	}
	col, err := s.store.GetCollectionByName(name)
	if err != nil {
		return ReindexResult{}, fmt.Errorf("collection %q not found", name)
	}

	vectorSpaceChanged := false
	if s.cfg != nil && s.cfg.Config.Embedding.Model != "" {
		status, err := s.store.ValidateEmbeddingProfile(ctx, store.ProfileFromConfig(s.cfg))
		if err != nil {
			return ReindexResult{}, fmt.Errorf("validate embedding profile: %w", err)
		}
		if status == store.ProfileMismatch {
			has, err := s.store.HasEmbeddedChunks(ctx)
			if err != nil {
				return ReindexResult{}, fmt.Errorf("check embedded chunks: %w", err)
			}
			if has {
				// The embedding profile is store-global: clearing it invalidates
				// every collection's embeddings, but the re-embed below only
				// refreshes the target collection. A collection-scoped reindex
				// must therefore refuse when OTHER collections still hold
				// chunks in the stored vector space — clearing the profile
				// would orphan their embeddings and the forced vector-index
				// rebuild would then hit a dimension mismatch (or silently mix
				// spaces). The full reindex path resets the whole store.
				otherHas, err := s.store.HasEmbeddedChunksExcept(ctx, col.ID)
				if err != nil {
					return ReindexResult{}, fmt.Errorf("check embedded chunks outside collection: %w", err)
				}
				if otherHas {
					return ReindexResult{}, fmt.Errorf(
						"%w: collection %q reindex would clear the store-wide embedding profile, but other collections still hold chunks embedded in a different vector space; use a store-wide reset: seek rm <collection> && seek add && seek embed -f",
						store.ErrProfileMismatch, name)
				}
				if !opts.AllowVectorSpaceChange {
					return ReindexResult{}, fmt.Errorf(
						"%w: stored embeddings were built with a different vector space; re-run with AllowVectorSpaceChange to clear and re-embed this collection, or reindex with: seek rm %q && seek add && seek embed -f",
						store.ErrProfileMismatch, name)
				}
				// Clear the profile; the force re-embed below establishes the
				// new vector space. Existing embeddings are rebuilt, never
				// mixed.
				if err := s.store.ClearEmbeddingProfile(ctx); err != nil {
					return ReindexResult{}, fmt.Errorf("clear embedding profile: %w", err)
				}
				vectorSpaceChanged = true
			}
			// An empty index carries nothing to invalidate: the pipeline's
			// ClaimEmbeddingProfile establishes the new vector space on the
			// first embedding write, matching the prior plan's contract.
		}
	}

	report, err := s.pipeline.Sync(ctx, col, pipeline.Options{
		Force:       true,
		VectorIndex: true,
	}, log)
	if err != nil {
		return ReindexResult{Report: report, VectorSpaceChanged: vectorSpaceChanged}, err
	}
	return ReindexResult{Report: report, VectorSpaceChanged: vectorSpaceChanged}, nil
}

// SyncPath validates that path is canonically inside the collection's
// registered path — rejecting outside paths, symlink/junction escapes, and
// Windows case-normalization escapes (plan §3.3) — and then runs the
// whole-collection sync through the collection's type-aware handler via the
// pipeline → indexer registry.
//
// The path is a security guard, NOT a scope filter: it does not restrict
// which files are indexed, so the returned report carries collection-wide
// counts. True per-path indexing (plumbing the path through the pipeline and
// every handler with per-path orphan/embedding semantics) is out of scope for
// this plan.
func (s *CollectionService) SyncPath(ctx context.Context, name, path string, opts pipeline.Options, log pipeline.Logger) (indexer.SyncReport, error) {
	if s == nil || s.store == nil || s.pipeline == nil {
		return indexer.SyncReport{}, fmt.Errorf("collection service: not configured")
	}
	col, err := s.store.GetCollectionByName(name)
	if err != nil {
		return indexer.SyncReport{}, fmt.Errorf("collection %q not found", name)
	}
	if err := ValidateCollectionPath(col, path); err != nil {
		return indexer.SyncReport{}, err
	}
	return s.pipeline.Sync(ctx, col, opts, log)
}

// Backfill runs a collection-scoped semantic fast-field backfill without
// touching source files, embeddings, the FTS index, or the vector index. It
// re-enriches every document whose semantic fingerprint is stale or absent
// from the already-indexed chunks and atomically updates the fast fields plus
// fingerprint/status (see indexer.BackfillSemantic). The CLI caller holds the
// writer lock (same model as sync/embed); cancellation via ctx stops the pass
// at the next document boundary.
func (s *CollectionService) Backfill(ctx context.Context, name string, log pipeline.Logger) (indexer.BackfillReport, error) {
	if s == nil || s.store == nil || s.indexer == nil {
		return indexer.BackfillReport{}, fmt.Errorf("collection service: not configured")
	}
	col, err := s.store.GetCollectionByName(name)
	if err != nil {
		return indexer.BackfillReport{}, fmt.Errorf("collection %q not found", name)
	}
	return s.indexer.BackfillSemantic(ctx, col, log)
}

// toCollectionInfo maps the persisted detail row to the presentation contract.
func toCollectionInfo(d store.CollectionDetail) CollectionInfo {
	return CollectionInfo{
		Name:            d.Name,
		Type:            d.Type,
		Path:            d.Path,
		Pattern:         d.Pattern,
		ParserName:      d.ParserName,
		ParserVersion:   d.ParserVersion,
		Backend:         d.Backend,
		Documents:       d.Documents,
		Chunks:          d.Chunks,
		EmbeddedChunks:  d.EmbeddedChunks,
		SemanticCurrent: d.SemanticCurrent,
		SemanticStale:   d.SemanticStale,
		SemanticError:   d.SemanticError,
		SemanticNone:    d.Documents - d.SemanticCurrent - d.SemanticStale - d.SemanticError,
	}
}
