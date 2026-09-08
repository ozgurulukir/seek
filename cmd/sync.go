package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/store"
)

type SyncCmd struct {
	Collection string `arg:"" optional:"" help:"Sync a specific collection (default: all)"`
	Type       string `help:"Sync only collections of this type"`
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
	return nil
}
