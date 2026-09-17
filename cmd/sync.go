package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ozgurulukir/seek/internal/agenthooks"
	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/pipeline"
)

type SyncCmd struct {
	Collection string `arg:"" optional:"" help:"Sync a specific collection (default: all)"`
	Type       string `help:"Sync only collections of this type"`
	Path       string `help:"Validate that PATH is inside the named collection, then sync the whole collection (security guard: the sync is collection-scoped, not path-scoped)"`
	NoEmbed    bool   `help:"Skip embedding newly synced chunks (keyword-only)"`
	Realtime   bool   `help:"Force the realtime request batch for embedding"`
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
		if err != nil {
			return fmt.Errorf("sync %q with path %q: %w", c.Collection, c.Path, err)
		}
		// The counts are collection-wide (the path is only a validated guard,
		// not the scope), so the message must not attribute them to the path.
		fmt.Printf("Synced %q: %d indexed, %d skipped, %d unsupported, %d failed (path %q validated inside collection)\n",
			c.Collection, report.Indexed, report.Skipped, report.Unsupported, report.Failed, c.Path)
		return nil
	}

	collections, err := runtime.Store.ListCollections()
	if err != nil {
		return err
	}

	if len(collections) == 0 {
		fmt.Println("No collections. Use 'seek add' to add one.")
		return nil
	}

	var failedNames []string

	for i := range collections {
		col := &collections[i]
		if c.Collection != "" && col.Name != c.Collection {
			continue
		}
		if c.Type != "" && string(col.Type) != c.Type {
			continue
		}

		fmt.Printf("Syncing %q (%s)...\n", col.Name, col.Type)

		_, err := runtime.Pipeline.Sync(ctx, col, pipeline.Options{
			Type:        c.Type,
			Realtime:    c.Realtime,
			VectorIndex: true,
			SkipEmbed:   c.NoEmbed,
		}, pipeline.NewStdoutLogger(os.Stderr))
		if err != nil {
			failedNames = append(failedNames, col.Name)
			fmt.Fprintf(os.Stderr, "  ERROR [%s]: %v\n", col.Name, err)
		}
	}

	if len(failedNames) > 0 {
		return fmt.Errorf("%d collection(s) failed to sync: %v", len(failedNames), strings.Join(failedNames, ", "))
	}

	return nil
}
