package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

type StatusCmd struct{}

func (c *StatusCmd) Run(cfg *config.AppConfig) (err error) {
	db, err := app.OpenStore(cfg)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	// Route through the shared CollectionService request path (one store
	// query) instead of the N+1 ListCollections + per-collection counts.
	collections, err := app.NewCollectionServiceFromStore(db).List(context.Background())
	if err != nil {
		return err
	}

	if len(collections) == 0 {
		fmt.Println("No collections. Use 'seek add' to add one.")
		return nil
	}

	fmt.Printf("Database: %s\n\n", cfg.DBPath)

	if err := printVectorSummary(db, cfg); err != nil {
		return err
	}

	for _, col := range collections {
		fmt.Printf("%-25s  type=%-10s  docs=%-5d  chunks=%-5d\n",
			col.Name, col.Type, col.Documents, col.Chunks)
		fmt.Printf("  → %s\n", formatRelPath(col.Path))
	}

	return nil
}

// printVectorSummary prints a short `vectors: model/dimensions, stale|ready`
// summary derived from the persisted embedding profile versus the current
// config. It never deletes data; a stale state only points at the reindex path.
func printVectorSummary(db *store.Store, cfg *config.AppConfig) error {
	desired := store.ProfileFromConfig(cfg)
	if desired.Model == "" {
		fmt.Println("vectors: not configured (set embedding.base_url/model/api_key)")
		return nil
	}
	stored, err := db.GetEmbeddingProfile(context.Background())
	if err != nil {
		return fmt.Errorf("read embedding profile: %w", err)
	}
	if stored == nil {
		fmt.Println("vectors: none (run: seek embed)")
		return nil
	}
	state := "ready"
	if stored.Fingerprint != desired.ComputeFingerprint() {
		state = "stale (reindex with: seek collection reindex --all --allow-vector-space-change)"
	}
	fmt.Printf("vectors: %s/%d, %s\n", stored.Model, stored.Dimensions, state)
	return nil
}
