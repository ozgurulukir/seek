//go:build windows

package renameio

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// TempFile creates a temporary file in the directory of path (or dir if specified).
func TempFile(dir, path string) (*PendingFile, error) {
	if dir == "" {
		dir = filepath.Dir(path)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return nil, err
	}
	return &PendingFile{
		File: f,
		path: path,
	}, nil
}

// CloseAtomicallyReplace syncs, closes, and uses MoveFileExW with MOVEFILE_REPLACE_EXISTING
// to atomically replace the destination file on Windows.
func (p *PendingFile) CloseAtomicallyReplace() error {
	if err := p.Sync(); err != nil {
		return err
	}
	p.closed = true
	if err := p.Close(); err != nil {
		return err
	}

	fromPtr, err := windows.UTF16PtrFromString(p.Name())
	if err != nil {
		return fmt.Errorf("convert src path to utf16: %w", err)
	}
	toPtr, err := windows.UTF16PtrFromString(p.path)
	if err != nil {
		return fmt.Errorf("convert dst path to utf16: %w", err)
	}

	flags := uint32(windows.MOVEFILE_REPLACE_EXISTING | windows.MOVEFILE_WRITE_THROUGH)
	if err := windows.MoveFileEx(fromPtr, toPtr, flags); err != nil {
		return fmt.Errorf("windows.MoveFileEx: %w", err)
	}

	p.done = true
	return nil
}

// Symlink atomically creates or replaces a symlink.
// Note: On Windows, creating symlinks requires Developer Mode enabled or SeCreateSymbolicLinkPrivilege.
func Symlink(oldname, newname string) error {
	_ = os.Remove(newname)
	return os.Symlink(oldname, newname)
}
