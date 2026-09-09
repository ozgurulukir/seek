package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/pipeline"
)

type SyncCmd struct {
	Collection string `arg:"" optional:"" help:"Sync a specific collection (default: all)"`
	Type       string `help:"Sync only collections of this type"`
	NoEmbed    bool   `help:"Skip embedding newly synced chunks (keyword-only)"`
	Realtime   bool   `help:"Embed with the realtime API instead of the async batch API"`
	NoLock     bool   `hidden:""`
}

func (c *SyncCmd) Run(cfg *config.AppConfig) (err error) {
	if c.NoLock && os.Getenv(hookLockEnv) != "1" {
		return fmt.Errorf("--no-lock is reserved for internal hook execution")
	}
	ctx := context.Background()
	if !c.NoLock {
		lockCtx, cancel := context.WithTimeout(ctx, hookLockWaitTimeout)
		defer cancel()
		lock, lockErr := acquireHookLock(lockCtx, hookSyncLockPath(cfg))
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

	collections, err := runtime.Store.ListCollections()
	if err != nil {
		return err
	}

	if len(collections) == 0 {
		fmt.Println("No collections. Use 'seek add' to add one.")
		return nil
	}

	idx := runtime.Indexer.WithContext(ctx)
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

		if err := idx.SyncCollectionContext(ctx, col); err != nil {
			failedNames = append(failedNames, col.Name)
			fmt.Printf("  ERROR [%s]: %v\n", col.Name, err)
		}
	}

	if len(failedNames) > 0 {
		return fmt.Errorf("%d collection(s) failed to sync: %v", len(failedNames), strings.Join(failedNames, ", "))
	}

	// M4: embed in the same process/store the sync just used. A missing
	// embedding capability is a configuration state, not a failure — the
	// pipeline warns once and leaves chunks pending (keyword search works).
	if !c.NoEmbed {
		if err := runtime.EmbedPending(ctx, pipeline.Options{
			Batch:       true, // default batch API, same as `seek embed`
			Realtime:    c.Realtime,
			Type:        c.Type,
			VectorIndex: true,
		}, pipeline.NewStdoutLogger(os.Stdout)); err != nil {
			return fmt.Errorf("embed pending chunks: %w", err)
		}
	}
	return nil
}
