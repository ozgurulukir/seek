package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/ozgurulukir/seek/internal/agenthooks"
	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
)

type RmCmd struct {
	Name string `arg:"" help:"Collection name to remove"`
}

func (c *RmCmd) Run(cfg *config.AppConfig) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), agenthooks.WriterLockTimeout)
	defer cancel()
	lock, err := agenthooks.AcquireWriterLock(ctx, agenthooks.WriterLockPath(cfg))
	if err != nil {
		return fmt.Errorf("acquire writer lock: %w", err)
	}
	defer lock.Close()

	db, err := app.OpenStore(cfg)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()

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
