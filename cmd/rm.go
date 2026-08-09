package cmd

import (
	"fmt"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

type RmCmd struct {
	Name string `arg:"" help:"Collection name to remove"`
}

func (c *RmCmd) Run(cfg *config.AppConfig) error {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	col, err := db.GetCollectionByName(c.Name)
	if err != nil {
		return fmt.Errorf("collection %q not found", c.Name)
	}

	docs, err := db.CountDocuments(col.ID)
	if err != nil {
		return fmt.Errorf("count documents: %w", err)
	}
	chunks, err := db.CountChunks(col.ID)
	if err != nil {
		return fmt.Errorf("count chunks: %w", err)
	}

	if err := db.DeleteCollection(col.ID); err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	fmt.Printf("Removed %q (%d docs, %d chunks)\n", col.Name, docs, chunks)
	return nil
}
