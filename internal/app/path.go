package app

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ozgurulukir/seek/internal/store"
)

// ValidateCollectionPath verifies that path is canonically under the
// collection's registered path before any index work dispatches. Both sides
// are made absolute and their symlinks/junctions are resolved, so a path that
// escapes the collection through a link (or a Windows junction) is rejected.
// On Windows the containment comparison is case-insensitive.
func ValidateCollectionPath(col *store.Collection, path string) error {
	switch {
	case col == nil:
		return fmt.Errorf("validate collection path: nil collection")
	case strings.TrimSpace(path) == "":
		return fmt.Errorf("validate collection path: empty path")
	case strings.TrimSpace(col.Path) == "":
		return fmt.Errorf("validate collection path: collection %q has no registered path", col.Name)
	}
	root, err := canonicalPath(col.Path)
	if err != nil {
		return fmt.Errorf("resolve collection path %q: %w", col.Path, err)
	}
	target, err := canonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}
	if !pathWithin(root, target) {
		return fmt.Errorf("path %q is outside collection %q (registered path %q)", path, col.Name, col.Path)
	}
	return nil
}

// canonicalPath returns an absolute, clean path with symlinks and junctions
// resolved. An unresolvable path (e.g. the target file is missing) falls back
// to the cleaned absolute path so containment validation still works for
// paths that may be created by the sync itself.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs, nil
	}
	return resolved, nil
}

// pathWithin reports whether target is root itself or a descendant of root.
// The comparison is case-insensitive on Windows (NTFS/Junction resolution is
// case-insensitive) and case-sensitive elsewhere.
func pathWithin(root, target string) bool {
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
		target = strings.ToLower(target)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
