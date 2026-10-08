package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ozgurulukir/seek/internal/agenthooks"
	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/pipeline"
	"github.com/ozgurulukir/seek/internal/source"
	"github.com/ozgurulukir/seek/internal/source/parserdef"
)

type SyncCmd struct {
	Collection string `arg:"" optional:"" help:"Sync a specific collection (default: all)"`
	Type       string `help:"Sync only collections of this type"`
	Path       string `help:"Validate that PATH is inside the named collection, then sync the whole collection (security guard: the sync is collection-scoped, not path-scoped)"`
	NoEmbed    bool   `help:"Skip embedding newly synced chunks (keyword-only)"`
	Realtime   bool   `help:"Force the realtime request batch for embedding"`
	JSON       bool   `help:"Emit collection outcomes as JSON (progress goes to stderr)"`
	Strict     bool   `help:"Fail on unavailable sources or skipped embeddings, as well as indexing errors"`
	NoLock     bool   `hidden:""`
}

func (c *SyncCmd) Run(cfg *config.AppConfig) (err error) {
	if c.NoLock && os.Getenv(agenthooks.LockEnv) != "1" {
		return fmt.Errorf("--no-lock is reserved for internal hook execution")
	}
	ctx, stop := commandContext()
	defer stop()
	if !c.NoLock {
		lockCtx, cancel := context.WithTimeout(ctx, agenthooks.WriterLockTimeout)
		defer cancel()
		lock, lockErr := agenthooks.AcquireWriterLock(lockCtx, agenthooks.WriterLockPath(cfg))
		if lockErr != nil {
			return fmt.Errorf("acquire writer lock: %w", lockErr)
		}
		defer lock.Close()
	}
	runtime, err := app.Open(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := runtime.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	for _, warning := range runtime.Warnings {
		fmt.Fprintf(os.Stderr, "WARN: %s\n", warning)
	}

	// --path is a security guard, not a scope filter (plan §3.3, accepted
	// design): the path is canonicalized and validated to be inside the named
	// collection (rejecting outside paths, symlink/junction escapes, and
	// Windows case-normalization escapes) BEFORE any index work, but the sync
	// itself runs over the whole collection — the path does not restrict which
	// files are indexed. The collection name is required (unlike a plain
	// `seek sync`, which defaults to all collections), and `--type` is
	// meaningless here since the dispatch is type-aware per collection.
	if c.Path != "" {
		if c.Collection == "" {
			return errors.New("sync --path requires a collection name: seek sync <collection> --path <path>")
		}
		if c.Type != "" {
			return errors.New("sync --path cannot be combined with --type; --path targets one named collection")
		}
		svc := app.NewCollectionService(runtime)
		report, err := svc.SyncPath(ctx, c.Collection, c.Path, pipeline.Options{
			Realtime:    c.Realtime,
			VectorIndex: true,
			SkipEmbed:   c.NoEmbed,
		}, pipeline.NewStdoutLogger(os.Stderr))
		outcome, failed := c.syncOutcome(cfg, report, err)
		if c.JSON {
			if emitErr := c.emitSyncOutcomes([]syncOutcome{outcome}); emitErr != nil {
				return emitErr
			}
		} else if err == nil {
			// Only a run that reached the indexer may claim the path guard was
			// honoured and report collection-wide counts; a validation or stage
			// error goes to stderr without a false success line on stdout.
			fmt.Printf("Synced %q: %d indexed, %d skipped, %d unsupported, %d failed (path %q validated inside collection; status=%s embeddings=%s)\n", c.Collection, outcome.Indexed, outcome.Skipped, outcome.Unsupported, outcome.Failed, c.Path, outcome.Status, outcome.Embeddings)
		}
		if failed {
			if err != nil {
				return fmt.Errorf("sync %q with path %q failed: %w", c.Collection, c.Path, err)
			}
			return fmt.Errorf("sync %q with path %q failed: %s", c.Collection, c.Path, outcome.Reason)
		}
		return nil
	}

	collections, err := runtime.Store.ListCollections()
	if err != nil {
		return err
	}

	var failedNames []string
	outcomes := make([]syncOutcome, 0, len(collections))

	for i := range collections {
		col := &collections[i]
		if c.Collection != "" && col.Name != c.Collection {
			continue
		}
		if c.Type != "" && string(col.Type) != c.Type {
			continue
		}

		fmt.Fprintf(os.Stderr, "Syncing %q (%s)...\n", col.Name, col.Type)

		report, err := runtime.Pipeline.Sync(ctx, col, pipeline.Options{
			Type:        c.Type,
			Realtime:    c.Realtime,
			VectorIndex: true,
			SkipEmbed:   c.NoEmbed,
		}, pipeline.NewStdoutLogger(os.Stderr))
		outcome, failed := c.syncOutcome(cfg, report, err)
		outcomes = append(outcomes, outcome)
		if failed {
			failedNames = append(failedNames, col.Name)
		}
		if outcome.Reason != "" {
			fmt.Fprintf(os.Stderr, "  %s [%s]: %s\n", outcome.Status, col.Name, outcome.Reason)
		}
	}
	if emitErr := c.emitSyncOutcomes(outcomes); emitErr != nil {
		return emitErr
	}
	if c.Collection != "" && len(outcomes) == 0 {
		return fmt.Errorf("collection %q not found or excluded by --type", c.Collection)
	}
	if len(failedNames) > 0 {
		return fmt.Errorf("%d collection(s) failed to sync: %v", len(failedNames), strings.Join(failedNames, ", "))
	}

	return nil
}

// syncOutcome retains the index report even when a subsequent stage fails.
type syncOutcome struct {
	indexer.SyncReport
	Status          string `json:"status"`
	Reason          string `json:"reason,omitempty"`
	Embeddings      string `json:"embeddings"`
	EmbeddingReason string `json:"embedding_reason,omitempty"`
}

func (c *SyncCmd) syncOutcome(cfg *config.AppConfig, report indexer.SyncReport, err error) (syncOutcome, bool) {
	out := syncOutcome{SyncReport: report, Status: "success", Embeddings: "not_run"}
	if err != nil {
		out.Reason = err.Error()
		if errors.Is(err, source.ErrUnavailable) || errors.Is(err, parserdef.ErrUnavailable) {
			out.Status = "skipped"
			out.Failed = 0
			out.Errors = nil
			out.Warnings++
			return out, c.Strict || c.Collection != "" || c.Type != ""
		}
		out.Status = "failed"
		return out, true
	}
	// The embedding stage only runs when the indexer returned no error, so a
	// per-file failure (which does not fail the collection) still leaves a real
	// embedding result to report.
	switch {
	case c.NoEmbed:
		out.Embeddings = "skipped_requested"
	default:
		if ok, reason := embed.EmbeddingCapability(cfg); !ok {
			out.Embeddings = "unavailable"
			out.EmbeddingReason = reason
		} else {
			out.Embeddings = "completed"
		}
	}
	if report.Failed > 0 {
		out.Status = "failed"
		out.Reason = fmt.Sprintf("%d indexing failures; see errors", report.Failed)
		return out, true
	}
	if out.Embeddings == "unavailable" {
		out.Status = "degraded"
		if c.Strict {
			out.Reason = out.EmbeddingReason
		}
		return out, c.Strict
	}
	return out, false
}

func (c *SyncCmd) emitSyncOutcomes(outcomes []syncOutcome) error {
	if c.JSON {
		return json.NewEncoder(os.Stdout).Encode(outcomes)
	}
	for _, out := range outcomes {
		fmt.Printf("Synced %q: %s (%d indexed, %d skipped, %d unsupported, %d failed; embeddings=%s)\n", out.Collection, out.Status, out.Indexed, out.Skipped, out.Unsupported, out.Failed, out.Embeddings)
	}
	if len(outcomes) == 0 && c.Collection == "" && c.Type == "" {
		fmt.Println("No collections. Use 'seek add' to add one.")
	}
	return nil
}
