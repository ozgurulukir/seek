//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package cmd

import (
	"os"
	"syscall"
)

// hookLock is an advisory lock. The operating system releases it when the
// hook process exits unexpectedly, so a crash cannot permanently block sync.
type hookLock struct {
	file *os.File
}

func lockHookFile(file *os.File) (*hookLock, error) {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, err
	}
	return &hookLock{file: file}, nil
}

func isHookLockBusy(err error) bool {
	return err == syscall.EWOULDBLOCK || err == syscall.EAGAIN
}

func (l *hookLock) Close() error {
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
