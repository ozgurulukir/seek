package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ozgurulukir/seek/internal/store"
)

// ValidateCollectionPath verifies that path is canonically under the
// collection's registered path before any index work dispatches. Both the
// collection root and the target are made absolute and their symlinks/junctions
// are resolved with FAIL-CLOSED semantics: a path that exists but cannot be
// resolved (a broken/dangling symlink, an access error, or a broken junction)
// is rejected rather than falling back to the unresolved path, so lexical
// containment is never the security boundary. A path that escapes the
// collection through a link (or a Windows junction) is therefore rejected.
// On Windows the containment comparison is case-insensitive.
//
// A target that does not exist yet (which sync may create) is accepted: the
// nearest existing ancestor is canonicalized and the remaining segments are
// appended, so containment is still checked against the resolved root.
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
// resolved, using FAIL-CLOSED semantics. It distinguishes existence via
// os.Lstat (which does not follow links, so a dangling symlink still counts as
// existing):
//
//   - Exists (real file/dir OR a symlink/junction): resolve it. If it cannot be
//     resolved (broken/dangling symlink, access error, broken junction) the
//     path is rejected rather than falling back to the unresolved abs, so a
//     link that could later become valid cannot bypass containment.
//   - Lstat fails for a reason other than not-exists (e.g. permission): reject.
//   - Does not exist: canonicalize the nearest existing ancestor and append the
//     remaining segments, so containment still validates paths that sync may
//     create.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err == nil {
		// Path exists (real file/dir OR a symlink/junction — Lstat does not
		// follow links, so a dangling symlink lands here). Resolve it.
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			// Exists but unresolvable: broken/dangling symlink, access error,
			// or broken junction. FAIL CLOSED — never fall back to the
			// unresolved path.
			return "", fmt.Errorf("resolve symlinks for %q: %w", abs, err)
		}
		return resolved, nil
	} else if !os.IsNotExist(err) {
		// Lstat failed for a reason other than not-exists (e.g. permission).
		// Fail closed.
		return "", fmt.Errorf("stat %q: %w", abs, err)
	}
	// Path does not exist. Canonicalize the nearest EXISTING ancestor and
	// safely append the remaining segments (sync may create the path itself).
	return canonicalizeNearestAncestor(abs)
}

// canonicalizeNearestAncestor canonicalizes a non-existent path by walking
// upward to the nearest EXISTING ancestor, resolving that ancestor's
// symlinks/junctions, and appending the remaining segments verbatim. If no
// ancestor exists at all (a fully non-existent path), the cleaned absolute path
// is returned unchanged — an error case for the collection-root call, whose
// path must exist.
func canonicalizeNearestAncestor(abs string) (string, error) {
	dir := abs
	for {
		if _, err := os.Lstat(dir); err == nil {
			// dir exists: resolve it and append the remaining segments.
			resolved, err := filepath.EvalSymlinks(dir)
			if err != nil {
				return "", fmt.Errorf("resolve symlinks for %q: %w", dir, err)
			}
			rest, err := filepath.Rel(dir, abs)
			if err != nil {
				// Rel can only fail across different drives on Windows; dir is
				// an ancestor of abs, so this should not happen.
				return "", fmt.Errorf("compute remaining path for %q: %w", abs, err)
			}
			return filepath.Join(resolved, rest), nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %q: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root without finding an existing ancestor.
			return abs, nil
		}
		dir = parent
	}
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
