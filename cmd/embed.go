package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/pipeline"
)

type EmbedCmd struct {
	Type     string `help:"Embed only chunks from collections of this type"`
	Force    bool   `short:"f" help:"Force re-embed all chunks"`
	Batch    bool   `short:"b" default:"true" help:"Use batch API (50% cheaper, async) — text-only models"`
	Realtime bool   `short:"r" help:"Use realtime API (synchronous, immediate)"`
	NoLock   bool   `hidden:""`
}

func (c *EmbedCmd) Run(cfg *config.AppConfig) (err error) {
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

	// M4: the full embed pass lives in internal/pipeline so `seek sync` and
	// stop hooks can run it in-process. This command is a thin wrapper.
	return runtime.EmbedPending(context.Background(), pipeline.Options{
		Force:       c.Force,
		Realtime:    c.Realtime,
		Batch:       c.Batch,
		Type:        c.Type,
		VectorIndex: true,
	}, pipeline.NewStdoutLogger(os.Stdout))
}
