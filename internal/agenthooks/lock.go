package agenthooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
)

// LockEnv marks child processes that run inside an already-held writer
// lock; `seek sync --no-lock` is rejected unless it is set, so the flag
// stays reserved for internal hook execution.
const LockEnv = "SEEK_INTERNAL_WRITER_LOCK"

// WriterLockTimeout bounds how long a sync waits for the multi-process
// writer lock.
const WriterLockTimeout = 5 * time.Minute

// Sync and embed budgets for hook-triggered runs.
const (
	hookSyncTimeout  = 3 * time.Minute
	hookEmbedTimeout = 90 * time.Second
)

// WriterLockPath returns the shared multi-process writer lock path.
func WriterLockPath(cfg *config.AppConfig) string {
	return filepath.Join(cfg.CacheDir, "hooks", "sync.lock")
}

// AcquireWriterLock takes the exclusive writer lock, waiting up to the
// context deadline for other seek processes to finish.
func AcquireWriterLock(ctx context.Context, path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), config.DefaultDirPerms); err != nil {
		return nil, err
	}
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, config.DefaultFilePerms)
		if err != nil {
			return nil, err
		}
		lock, err := lockHookFile(file)
		if err == nil {
			return lock, nil
		}
		_ = file.Close()
		if !isHookLockBusy(err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// withLockEnv marks an environment so the child knows the writer lock is
// already held, replacing any stale value.
func withLockEnv(env []string) []string {
	filtered := env[:0]
	for _, value := range env {
		if !strings.HasPrefix(value, LockEnv+"=") {
			filtered = append(filtered, value)
		}
	}
	return append(filtered, LockEnv+"=1")
}

// withoutLockEnv strips the marker so a detached child acquires the lock
// itself.
func withoutLockEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, value := range env {
		if !strings.HasPrefix(value, LockEnv+"=") {
			filtered = append(filtered, value)
		}
	}
	return filtered
}
