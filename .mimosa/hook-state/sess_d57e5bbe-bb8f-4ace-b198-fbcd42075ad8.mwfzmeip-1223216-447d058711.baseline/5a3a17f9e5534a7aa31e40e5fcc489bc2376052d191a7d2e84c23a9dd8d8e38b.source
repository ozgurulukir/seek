package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
)

func TestValidateCollectionPathOutOfBounds(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "sub", "note.md")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}

	if err := ValidateCollectionPath(col, inside); err != nil {
		t.Errorf("inside path rejected: %v", err)
	}
	if err := ValidateCollectionPath(col, root); err != nil {
		t.Errorf("collection root itself rejected: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(outside, []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCollectionPath(col, outside); err == nil {
		t.Error("outside path accepted")
	}

	// A parent-escape path is rejected even though the file exists.
	escape := filepath.Join(root, "..", "escape.md")
	if err := os.WriteFile(escape, []byte("# x"), 0o644); err == nil {
		defer os.Remove(escape)
		if err := ValidateCollectionPath(col, escape); err == nil {
			t.Error("parent-escape path accepted")
		}
	}

	// Ellipsis boundary: sibling directory with a shared prefix must be outside.
	sibling := root + "-sibling"
	if err := os.MkdirAll(sibling, 0o755); err == nil {
		defer os.RemoveAll(sibling)
		sibFile := filepath.Join(sibling, "note.md")
		if err := os.WriteFile(sibFile, []byte("# hi"), 0o644); err == nil {
			if err := ValidateCollectionPath(col, sibFile); err == nil {
				t.Error("sibling-with-prefix path accepted as inside")
			}
		}
	}
}

func TestValidateCollectionPathSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDir, "note.md"), []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outsideDir, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation requires privileges on Windows")
		}
		t.Fatalf("create symlink: %v", err)
	}
	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}
	if err := ValidateCollectionPath(col, filepath.Join(link, "note.md")); err == nil {
		t.Error("symlink escape accepted")
	}
}

// TestValidateCollectionPathDanglingSymlinkRejected pins the fail-closed fix:
// a dangling symlink (link entry exists, target does not) must be rejected,
// never accepted via lexical containment. EvalSymlinks fails on it, so the
// unresolved path must NOT be used as a fallback.
func TestValidateCollectionPathDanglingSymlinkRejected(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(filepath.Join(t.TempDir(), "nowhere"), link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation requires privileges on Windows")
		}
		t.Fatalf("create symlink: %v", err)
	}
	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}
	if err := ValidateCollectionPath(col, link); err == nil {
		t.Error("dangling symlink path accepted (must be rejected — fail closed)")
	}
}

// TestValidateCollectionPathNonExistingTargetAccepted verifies that a path
// which does not exist yet (which sync will create) is accepted and resolves
// to the correct canonical path: the resolved collection root joined with the
// remaining segments.
func TestValidateCollectionPathNonExistingTargetAccepted(t *testing.T) {
	root := t.TempDir()
	// A real file inside the collection so the ancestor chain resolves.
	if err := os.WriteFile(filepath.Join(root, "existing.md"), []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := filepath.Join(root, "sub", "new-note.md")

	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}
	if err := ValidateCollectionPath(col, future); err != nil {
		t.Fatalf("non-existent target under collection root rejected: %v", err)
	}

	// It must canonicalize to the resolved root joined with the remaining
	// segments (sub/new-note.md), not the unresolved abs.
	rootResolved, err := canonicalPath(root)
	if err != nil {
		t.Fatalf("canonicalPath(root): %v", err)
	}
	got, err := canonicalPath(future)
	if err != nil {
		t.Fatalf("canonicalPath(future): %v", err)
	}
	rel, err := filepath.Rel(root, future)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	want := filepath.Join(rootResolved, rel)
	if got != want {
		t.Errorf("canonicalPath(%q) = %q, want %q", future, got, want)
	}
}

func TestValidateCollectionPathWindowsCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive containment is Windows-specific")
	}
	root := t.TempDir()
	inside := filepath.Join(root, "Note.md")
	if err := os.WriteFile(inside, []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}
	mixed := filepath.Join(root, "note.MD")
	if err := ValidateCollectionPath(col, mixed); err != nil {
		t.Errorf("case-mismatched path rejected on Windows: %v", err)
	}
}

func TestValidateCollectionPathInvalidInputs(t *testing.T) {
	root := t.TempDir()
	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}

	tests := []struct {
		name    string
		col     *store.Collection
		path    string
		wantErr string
	}{
		{
			name:    "nil collection",
			col:     nil,
			path:    filepath.Join(root, "doc.md"),
			wantErr: "nil collection",
		},
		{
			name:    "empty target path",
			col:     col,
			path:    "",
			wantErr: "empty path",
		},
		{
			name:    "whitespace target path",
			col:     col,
			path:    "   \t  ",
			wantErr: "empty path",
		},
		{
			name:    "empty collection path",
			col:     &store.Collection{Name: "notes", Path: ""},
			path:    filepath.Join(root, "doc.md"),
			wantErr: "has no registered path",
		},
		{
			name:    "whitespace collection path",
			col:     &store.Collection{Name: "notes", Path: "   "},
			path:    filepath.Join(root, "doc.md"),
			wantErr: "has no registered path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCollectionPath(tt.col, tt.path)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateCollectionPathUnresolvablePaths(t *testing.T) {
	root := t.TempDir()
	col := &store.Collection{Name: "notes", Path: root, Type: store.CollectionTypeMarkdown}

	// Unresolvable target path (dangling symlink)
	dangling := filepath.Join(root, "broken_link")
	if err := os.Symlink(filepath.Join(root, "nonexistent_target"), dangling); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks require admin privileges on Windows")
		}
		t.Fatalf("create symlink: %v", err)
	}

	err := ValidateCollectionPath(col, dangling)
	if err == nil {
		t.Fatal("expected error for dangling target symlink, got nil")
	}
	if !strings.Contains(err.Error(), "resolve path") {
		t.Errorf("expected 'resolve path' error, got %q", err.Error())
	}

	// Unresolvable collection path (dangling symlink as col.Path)
	colDangling := &store.Collection{Name: "broken_col", Path: dangling, Type: store.CollectionTypeMarkdown}
	validTarget := filepath.Join(root, "valid.md")
	if err := os.WriteFile(validTarget, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = ValidateCollectionPath(colDangling, validTarget)
	if err == nil {
		t.Fatal("expected error for dangling collection path, got nil")
	}
	if !strings.Contains(err.Error(), "resolve collection path") {
		t.Errorf("expected 'resolve collection path' error, got %q", err.Error())
	}
}

func TestCanonicalPath(t *testing.T) {
	root := t.TempDir()

	// 1. Existing real file
	filePath := filepath.Join(root, "real.txt")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalPath(filePath)
	if err != nil {
		t.Fatalf("canonicalPath real file: %v", err)
	}
	evalWant, _ := filepath.EvalSymlinks(filePath)
	if got != evalWant {
		t.Errorf("canonicalPath(%q) = %q, want %q", filePath, got, evalWant)
	}

	// 2. Symlinked file/dir
	targetDir := t.TempDir()
	linkDir := filepath.Join(root, "link_dir")
	if err := os.Symlink(targetDir, linkDir); err == nil {
		gotLink, err := canonicalPath(linkDir)
		if err != nil {
			t.Fatalf("canonicalPath symlink dir: %v", err)
		}
		targetResolved, _ := filepath.EvalSymlinks(targetDir)
		if gotLink != targetResolved {
			t.Errorf("canonicalPath symlink = %q, want %q", gotLink, targetResolved)
		}
	} else if runtime.GOOS != "windows" {
		t.Fatalf("os.Symlink: %v", err)
	}

	// 3. Dangling symlink
	danglingLink := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(root, "missing"), danglingLink); err == nil {
		_, err := canonicalPath(danglingLink)
		if err == nil {
			t.Errorf("canonicalPath on dangling symlink expected error, got nil")
		} else if !strings.Contains(err.Error(), "resolve symlinks for") {
			t.Errorf("canonicalPath unexpected error: %v", err)
		}
	}

	// 4. Non-existent path with existing ancestor
	nonExistent := filepath.Join(root, "dir1", "dir2", "file.txt")
	gotNonExistent, err := canonicalPath(nonExistent)
	if err != nil {
		t.Fatalf("canonicalPath non-existent path: %v", err)
	}
	rootResolved, _ := filepath.EvalSymlinks(root)
	wantNonExistent := filepath.Join(rootResolved, "dir1", "dir2", "file.txt")
	if gotNonExistent != wantNonExistent {
		t.Errorf("canonicalPath non-existent = %q, want %q", gotNonExistent, wantNonExistent)
	}

	// 5. Non-existent path with symlinked ancestor
	if linkDirResolved, err := filepath.EvalSymlinks(targetDir); err == nil && linkDir != "" {
		if _, err := os.Lstat(linkDir); err == nil {
			nestedNonExistent := filepath.Join(linkDir, "sub", "file.txt")
			gotNested, err := canonicalPath(nestedNonExistent)
			if err != nil {
				t.Fatalf("canonicalPath with symlinked ancestor: %v", err)
			}
			wantNested := filepath.Join(linkDirResolved, "sub", "file.txt")
			if gotNested != wantNested {
				t.Errorf("canonicalPath symlinked ancestor = %q, want %q", gotNested, wantNested)
			}
		}
	}
}

func TestCanonicalizeNearestAncestorRootNotExists(t *testing.T) {
	var fakePath string
	if runtime.GOOS == "windows" {
		fakePath = `Z:\nonexistent_drive_root\sub\file.txt`
	} else {
		fakePath = "/some/nonexistent/deep/path"
	}

	res, err := canonicalizeNearestAncestor(fakePath)
	if err != nil {
		t.Fatalf("canonicalizeNearestAncestor error: %v", err)
	}
	if res == "" {
		t.Error("expected non-empty canonical path")
	}
}

func TestPathWithin(t *testing.T) {
	root := filepath.Join("a", "b", "c")

	tests := []struct {
		name   string
		root   string
		target string
		want   bool
	}{
		{
			name:   "exact match",
			root:   root,
			target: root,
			want:   true,
		},
		{
			name:   "child path",
			root:   root,
			target: filepath.Join(root, "d", "e.txt"),
			want:   true,
		},
		{
			name:   "parent path",
			root:   root,
			target: filepath.Join("a", "b"),
			want:   false,
		},
		{
			name:   "sibling with prefix",
			root:   root,
			target: root + "-sibling",
			want:   false,
		},
		{
			name:   "relative escape",
			root:   root,
			target: filepath.Join(root, "..", "other"),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pathWithin(tt.root, tt.target)
			if got != tt.want {
				t.Errorf("pathWithin(%q, %q) = %v, want %v", tt.root, tt.target, got, tt.want)
			}
		})
	}

	upperRoot := filepath.Join("A", "B", "C")
	lowerTarget := filepath.Join("a", "b", "c", "d.txt")
	gotCase := pathWithin(upperRoot, lowerTarget)
	if runtime.GOOS == "windows" {
		if !gotCase {
			t.Errorf("pathWithin(%q, %q) on Windows should be true", upperRoot, lowerTarget)
		}
	} else {
		if gotCase {
			t.Errorf("pathWithin(%q, %q) on Unix should be false due to case sensitivity", upperRoot, lowerTarget)
		}
	}
}
