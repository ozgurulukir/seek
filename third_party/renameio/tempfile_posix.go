//go:build !windows

package renameio

import (
	"os"
	"path/filepath"
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

// CloseAtomicallyReplace syncs, closes, and renames the temp file over destination.
func (p *PendingFile) CloseAtomicallyReplace() error {
	if err := p.Sync(); err != nil {
		return err
	}
	p.closed = true
	if err := p.Close(); err != nil {
		return err
	}
	if err := os.Rename(p.Name(), p.path); err != nil {
		return err
	}
	p.done = true
	return nil
}

// Symlink atomically creates or replaces a symlink.
func Symlink(oldname, newname string) error {
	_ = os.Remove(newname)
	return os.Symlink(oldname, newname)
}
