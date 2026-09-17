package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// commandContext returns a base context canceled on SIGINT/SIGTERM, plus the
// matching stop function (defer it). Long-running and destructive commands
// (sync, embed, reindex, rm, search) derive their contexts from it so Ctrl+C
// unwinds through the pipeline's cancellation-aware paths and the deferred
// cleanup — writer lock release, runtime close, partial-state recovery —
// runs, instead of the process dying mid-write (review 2026-09-17 M7).
func commandContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
