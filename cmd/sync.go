package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/pipeline"
	"github.com/ozgurulukir/seek/internal/store"
)

type SyncCmd struct {
	Collection string `arg:"" optional:"" help:"Sync a specific collection (default: all)"`
	Type       string `help:"Sync only collections of this type"`
	NoEmbed    bool   `help:"Skip embedding newly synced chunks (keyword-only)"`
	Realtime   bool   `help:"Embed with the realtime API instead of the async batch API"`
	NoLock     bool   `hidden:""`
}

func (c *SyncCmd) Run(cfg *config.AppConfig) error {
	if c.NoLock && os.Getenv(hookLockEnv) != "1" {
		return fmt.Errorf("--no-lock is reserved for internal hook execution")
	}
	if !c.NoLock {
		ctx, cancel := context.WithTimeout(context.Background(), hookLockWaitTimeout)
		defer cancel()
		lock, err := acquireHookLock(ctx, hookSyncLockPath(cfg))
		if err != nil {
			return fmt.Errorf("acquire writer lock: %w", err)
		}
		defer lock.Close()
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	collections, err := db.ListCollections()
	if err != nil {
		return err
	}

	if len(collections) == 0 {
		fmt.Println("No collections. Use 'seek add' to add one.")
		return nil
	}

	idx := indexer.New(cfg, db)
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

		if err := idx.SyncCollection(col); err != nil {
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
		vectorIndex := false
		if cfg.Config.VectorIndex.Backend != "" && cfg.Config.VectorIndex.Backend != "linear" {
			if vi, err := store.NewVectorIndex(cfg); err == nil {
				db.SetVectorIndex(vi)
				vectorIndex = true
			}
		}
		if err := pipeline.EmbedPending(cfg, db, pipeline.Options{
			Batch:       true, // default batch API, same as `seek embed`
			Realtime:    c.Realtime,
			Type:        c.Type,
			VectorIndex: vectorIndex,
		}, pipeline.NewStdoutLogger(os.Stdout)); err != nil {
			return fmt.Errorf("embed pending chunks: %w", err)
		}
	}
	return nil
}
