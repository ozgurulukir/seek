//go:build windows

package cmd

import (
	"os"

	"golang.org/x/sys/windows"
)

// hookLock is an advisory lock. Windows releases it when the hook process
// exits unexpectedly, so a crash cannot permanently block sync.
type hookLock struct {
	file       *os.File
	overlapped windows.Overlapped
}

func lockHookFile(file *os.File) (*hookLock, error) {
	lock := &hookLock{file: file}
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock.overlapped)
	if err != nil {
		return nil, err
	}
	return lock, nil
}

func isHookLockBusy(err error) bool {
	return err == windows.ERROR_LOCK_VIOLATION
}

func (l *hookLock) Close() error {
	err := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &l.overlapped)
	closeErr := l.file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
