//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package agenthooks

import (
	"errors"
	"os"
)

// Unsupported targets compile cleanly but report that the multi-process lock
// is unavailable instead of silently running without writer coordination.
type Lock struct {
	file *os.File
}

func lockHookFile(file *os.File) (*Lock, error) {
	return nil, errors.New("seek hook writer lock is unsupported on this OS")
}

func (l *Lock) Close() error {
	return l.file.Close()
}

func isHookLockBusy(error) bool {
	return false
}
