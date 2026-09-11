//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package agenthooks

import (
	"os"
	"syscall"
)

// Lock is an advisory lock. The operating system releases it when the
// hook process exits unexpectedly, so a crash cannot permanently block sync.
type Lock struct {
	file *os.File
}

func lockHookFile(file *os.File) (*Lock, error) {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, err
	}
	return &Lock{file: file}, nil
}

func isHookLockBusy(err error) bool {
	return err == syscall.EWOULDBLOCK || err == syscall.EAGAIN
}

func (l *Lock) Close() error {
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
