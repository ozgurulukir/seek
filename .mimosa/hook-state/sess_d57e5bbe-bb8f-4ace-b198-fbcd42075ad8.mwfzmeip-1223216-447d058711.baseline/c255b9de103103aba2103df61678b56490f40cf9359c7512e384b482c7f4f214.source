package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ozgurulukir/seek/internal/agenthooks"
	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/pipeline"
	"github.com/ozgurulukir/seek/internal/store"
)

// CollectionCmd is the collection lifecycle surface (plan §3.3). It manages
// the index only: rename and reindex never touch the source files, which
// remain the source of truth. Older `seek status`/`seek rm` stay as
// compatibility aliases.
type CollectionCmd struct {
	List    CollectionListCmd    `cmd:"" help:"List all collections with semantic coverage"`
	Show    CollectionShowCmd    `cmd:"" help:"Show detailed information for one collection"`
	Rename  CollectionRenameCmd  `cmd:"" help:"Rename a collection (index label only; source path unchanged)"`
	Reindex CollectionReindexCmd `cmd:"" help:"Rebuild a collection from its source files"`
}

// CollectionListCmd implements `seek collection list`.
type CollectionListCmd struct {
	JSON bool `help:"Output in JSON format"`
}

// CollectionShowCmd implements `seek collection show <name>`.
type CollectionShowCmd struct {
	Name string `arg:"" help:"Collection name"`
	JSON bool   `help:"Output in JSON format"`
}

// CollectionRenameCmd implements `seek collection rename <old> <new>`.
type CollectionRenameCmd struct {
	Old string `arg:"" help:"Current collection name"`
	New string `arg:"" help:"New collection name"`
}

// CollectionReindexCmd implements `seek collection reindex <name>` and
// `seek collection reindex --all`.
//
// The full pass re-reads the collection's source files and rebuilds the
// FTS/chunk/fast-field lifecycle, then re-embeds its chunks. The
// --semantic-only pass is the S3 backfill: it re-enriches already-indexed
// chunks and only writes fast fields plus the semantic fingerprint/status —
// source files, embeddings, FTS, and the vector index are untouched.
//
// Name is an optional positional so it can be omitted with --all; kong does
// not support conditionally-optional positionals, so the Name XOR --all
// invariant is enforced at runtime in Run.
type CollectionReindexCmd struct {
	Name                   string `arg:"" optional:"" help:"Collection name (omit with --all)"`
	SemanticOnly           bool   `help:"Backfill semantic fast fields only; source files, embeddings, FTS, and the vector index are untouched"`
	AllowVectorSpaceChange bool   `help:"Permit clearing the embedding profile and re-embedding when the stored vector space mismatches the current config"`
	All                    bool   `help:"Reindex every collection (store-wide recovery); requires --allow-vector-space-change"`
}

func (c *CollectionListCmd) Run(cfg *config.AppConfig) (err error) {
	// list/show are pure store reads: open the lightweight SQLite-only path
	// (same as `seek status`/`fields`/`rm`) instead of the full runtime, so
	// an unconfigured or broken embedding provider or vector index cannot
	// break or slow down a read-only query (review M3).
	db, err := app.OpenStore(cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	infos, err := app.NewCollectionServiceFromStore(db).List(context.Background())
	if err != nil {
		return err
	}

	if c.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if infos == nil {
			infos = []app.CollectionInfo{}
		}
		return enc.Encode(infos)
	}

	if len(infos) == 0 {
		fmt.Println("No collections. Use 'seek add' to add one.")
		return nil
	}

	fmt.Printf("Database: %s\n\n", cfg.DBPath)
	for _, info := range infos {
		printCollectionListLine(info)
	}
	return nil
}

func (c *CollectionShowCmd) Run(cfg *config.AppConfig) (err error) {
	db, err := app.OpenStore(cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	info, err := app.NewCollectionServiceFromStore(db).Show(context.Background(), c.Name)
	if err != nil {
		return err
	}

	if c.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	printCollectionShow(info)
	return nil
}

func (c *CollectionRenameCmd) Run(cfg *config.AppConfig) (err error) {
	// Renaming a collection to itself is a no-op; report it as such instead of
	// printing a misleading "renamed" success message.
	if c.Old == c.New {
		fmt.Printf("Collection %q name unchanged.\n", c.Old)
		return nil
	}

	lockCtx, cancel := context.WithTimeout(context.Background(), agenthooks.WriterLockTimeout)
	defer cancel()
	lock, err := agenthooks.AcquireWriterLock(lockCtx, agenthooks.WriterLockPath(cfg))
	if err != nil {
		return fmt.Errorf("acquire writer lock: %w", err)
	}
	defer lock.Close()

	runtime, err := app.Open(cfg)
	if err != nil {
		return err
	}
	defer closeCollectionRuntime(&err, runtime)

	// Resolve the source path up front so the success message can confirm it
	// stayed unchanged (rename never rewrites source files). Only a genuine
	// miss is "not found" — a store failure must keep its cause instead of
	// being misreported (review 2026-09-17 L16).
	col, err := runtime.Store.GetCollectionByName(c.Old)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("collection %q not found", c.Old)
		}
		return fmt.Errorf("load collection %q: %w", c.Old, err)
	}

	svc := app.NewCollectionService(runtime)
	if err := svc.Rename(context.Background(), c.Old, c.New); err != nil {
		if errors.Is(err, store.ErrCollectionExists) {
			return fmt.Errorf("cannot rename %q to %q: a collection named %q already exists; choose a different name", c.Old, c.New, c.New)
		}
		return fmt.Errorf("rename collection %q: %w", c.Old, err)
	}

	fmt.Printf("Renamed collection %q to %q. Source path unchanged: %s\n",
		c.Old, c.New, formatRelPath(col.Path))
	return nil
}

func (c *CollectionReindexCmd) Run(cfg *config.AppConfig) (err error) {
	// Name XOR --all: exactly one must be provided. kong cannot express a
	// conditionally-optional positional, so this is enforced at runtime.
	if c.All && c.Name != "" {
		return fmt.Errorf("reindex: specify a collection name OR --all, not both")
	}
	if !c.All && c.Name == "" {
		return fmt.Errorf("reindex: a collection name is required, or use --all to reindex every collection")
	}
	// --semantic-only is collection-scoped (Backfill takes a single collection);
	// --all reindexes every collection. They conflict, so reject the pair.
	if c.All && c.SemanticOnly {
		return fmt.Errorf("--semantic-only cannot be combined with --all: --semantic-only is collection-scoped")
	}

	// The semantic-only backfill writes fast fields, so both passes take the
	// writer lock (same model as sync/embed).
	ctx, stop := commandContext()
	defer stop()
	lockCtx, cancel := context.WithTimeout(ctx, agenthooks.WriterLockTimeout)
	defer cancel()
	lock, err := agenthooks.AcquireWriterLock(lockCtx, agenthooks.WriterLockPath(cfg))
	if err != nil {
		return fmt.Errorf("acquire writer lock: %w", err)
	}
	defer lock.Close()

	runtime, err := app.Open(cfg)
	if err != nil {
		return err
	}
	defer closeCollectionRuntime(&err, runtime)

	svc := app.NewCollectionService(runtime)
	log := pipeline.NewStdoutLogger(os.Stderr)

	if c.SemanticOnly {
		report, err := svc.Backfill(ctx, c.Name, log)
		if err != nil {
			return fmt.Errorf("reindex %q (--semantic-only): %w", c.Name, err)
		}
		fmt.Printf("Semantic backfill for %q: %d processed, %d skipped, %d failed\n",
			c.Name, report.Processed, report.Skipped, report.Failed)
		return nil
	}

	if c.All {
		results, err := svc.ReindexAll(ctx, app.ReindexOptions{
			AllowVectorSpaceChange: c.AllowVectorSpaceChange,
		}, log)
		if err != nil {
			// A profile mismatch (store.ErrProfileMismatch) already carries the
			// reindex hint in its text; wrapping preserves and surfaces it.
			return fmt.Errorf("reindex --all: %w", err)
		}
		changed := 0
		for _, res := range results {
			fmt.Printf("Reindexed %q: %d indexed, %d skipped, %d unsupported, %d failed\n",
				res.Report.Collection, res.Report.Indexed, res.Report.Skipped, res.Report.Unsupported, res.Report.Failed)
			if res.VectorSpaceChanged {
				changed++
			}
		}
		if changed > 0 {
			fmt.Printf("Vector space changed for %d collection(s): cleared the old embedding profile and re-embedded.\n", changed)
		}
		return nil
	}

	res, err := svc.Reindex(ctx, c.Name, app.ReindexOptions{
		AllowVectorSpaceChange: c.AllowVectorSpaceChange,
	}, log)
	if err != nil {
		// A profile mismatch (store.ErrProfileMismatch) already carries the
		// reindex hint in its text; wrapping preserves and surfaces it.
		return fmt.Errorf("reindex %q: %w", c.Name, err)
	}

	report := res.Report
	fmt.Printf("Reindexed %q: %d indexed, %d skipped, %d unsupported, %d failed\n",
		c.Name, report.Indexed, report.Skipped, report.Unsupported, report.Failed)
	if res.VectorSpaceChanged {
		fmt.Println("Vector space changed: cleared the old embedding profile and re-embedded the collection.")
	}
	return nil
}

// closeCollectionRuntime joins any runtime close error into the returned
// error, following the pattern used by the other composition-root commands.
func closeCollectionRuntime(err *error, runtime *app.Runtime) {
	if closeErr := runtime.Close(); closeErr != nil {
		*err = errors.Join(*err, closeErr)
	}
}

// printCollectionListLine renders one collection summary in the argument
// style (name, type, counts, semantic coverage, path). Semantic coverage is
// "current/stale/error/none"; none = documents with no recorded state.
func printCollectionListLine(info app.CollectionInfo) {
	fmt.Printf("%-25s  type=%-10s  docs=%-5d  chunks=%-5d  embedded=%-5d  semantic=%s\n",
		info.Name, info.Type, info.Documents, info.Chunks, info.EmbeddedChunks, formatSemanticCoverage(info))
	fmt.Printf("  \u2192 %s\n", formatRelPath(info.Path))
}

// formatSemanticCoverage renders the four semantic counts as
// "c/s/e/n (current/stale/error/none)" so list stays compact yet complete.
func formatSemanticCoverage(info app.CollectionInfo) string {
	return fmt.Sprintf("%d/%d/%d/%d (current/stale/error/none)",
		info.SemanticCurrent, info.SemanticStale, info.SemanticError, info.SemanticNone)
}

// printCollectionShow renders the detailed view for one collection,
// including every field the plan's `show` surface promises.
func printCollectionShow(info app.CollectionInfo) {
	fmt.Printf("Name:         %s\n", info.Name)
	fmt.Printf("Type:         %s\n", info.Type)
	fmt.Printf("Path:         %s\n", formatRelPath(info.Path))
	if info.Pattern != "" {
		fmt.Printf("Pattern:      %s\n", info.Pattern)
	}
	if info.ParserName != "" {
		fmt.Printf("Parser:       %s (v%d)\n", info.ParserName, info.ParserVersion)
	} else {
		fmt.Println("Parser:       (built-in)")
	}
	if info.Backend != "" {
		fmt.Printf("Backend:      %s\n", info.Backend)
	}
	fmt.Printf("Documents:    %d\n", info.Documents)
	fmt.Printf("Chunks:       %d\n", info.Chunks)
	fmt.Printf("Embedded:     %d\n", info.EmbeddedChunks)
	fmt.Printf("Semantic:     %s\n", formatSemanticCoverage(info))
	if len(info.LastSyncErrors) > 0 {
		fmt.Printf("Last Sync Errors:\n  %s\n", strings.Join(info.LastSyncErrors, "\n  "))
	}
}
