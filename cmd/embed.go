package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/pipeline"
	"github.com/ozgurulukir/seek/internal/store"
)

type EmbedCmd struct {
	Type     string `help:"Embed only chunks from collections of this type"`
	Force    bool   `short:"f" help:"Force re-embed all chunks"`
	Batch    bool   `short:"b" default:"true" help:"Use batch API (50% cheaper, async) — text-only models"`
	Realtime bool   `short:"r" help:"Use realtime API (synchronous, immediate)"`
	NoLock   bool   `hidden:""`
}

func (c *EmbedCmd) Run(cfg *config.AppConfig) error {
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

	vectorIndex := false
	if cfg.Config.VectorIndex.Backend != "" && cfg.Config.VectorIndex.Backend != "linear" {
		if vi, err := store.NewVectorIndex(cfg); err == nil {
			db.SetVectorIndex(vi)
			vectorIndex = true
		}
	}
	db.SetCompression(cfg.Config.Compression.Algorithm != "", cfg.Config.Compression.Level)

	// M4: the full embed pass lives in internal/pipeline so `seek sync` and
	// stop hooks can run it in-process. This command is a thin wrapper.
	return pipeline.EmbedPending(cfg, db, pipeline.Options{
		Force:       c.Force,
		Realtime:    c.Realtime,
		Batch:       c.Batch,
		Type:        c.Type,
		VectorIndex: vectorIndex,
	}, pipeline.NewStdoutLogger(os.Stdout))
}
