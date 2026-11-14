//go:build windows

package agenthooks

import (
	"os"

	"golang.org/x/sys/windows"
)

// Lock is an advisory lock. Windows releases it when the hook process
// exits unexpectedly, so a crash cannot permanently block sync.
type Lock struct {
	file       *os.File
	overlapped windows.Overlapped
}

func lockHookFile(file *os.File) (*Lock, error) {
	lock := &Lock{file: file}
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock.overlapped)
	if err != nil {
		return nil, err
	}
	return lock, nil
}

func isHookLockBusy(err error) bool {
	return err == windows.ERROR_LOCK_VIOLATION
}

func (l *Lock) Close() error {
	err := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &l.overlapped)
	closeErr := l.file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
