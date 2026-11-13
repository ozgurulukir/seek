package renameio

import "os"

// PendingFile represents a pending file that will atomically replace the target destination.
type PendingFile struct {
	*os.File
	path   string
	closed bool
	done   bool
}

// Cleanup closes and removes the temporary file unless already successfully replaced.
func (p *PendingFile) Cleanup() error {
	if p.done {
		return nil
	}
	var closeErr error
	if !p.closed {
		closeErr = p.Close()
		p.closed = true
	}
	if err := os.Remove(p.Name()); err != nil {
		return err
	}
	return closeErr
}

// WriteFile writes data to a temporary file and atomically replaces filename.
func WriteFile(filename string, data []byte, perm os.FileMode) error {
	t, err := TempFile("", filename)
	if err != nil {
		return err
	}
	defer t.Cleanup()

	if err := t.Chmod(perm); err != nil {
		return err
	}

	if _, err := t.Write(data); err != nil {
		return err
	}

	return t.CloseAtomicallyReplace()
}
