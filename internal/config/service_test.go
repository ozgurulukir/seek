package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestService_ReadWriteExists round-trips Read/Write/Exists on the default
// config path (rooted at the temp HOME set by the test).
func TestService_ReadWriteExists(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	svc := NewService()
	if svc.Exists() {
		t.Fatalf("config should not exist before Write")
	}
	if _, err := svc.Read(); err == nil {
		t.Fatalf("Read should error when the file is absent")
	}

	cfg := Config{Embedding: EmbeddingConfig{Model: "text-embedding-3-small"}}
	if err := svc.Write(cfg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !svc.Exists() {
		t.Fatalf("config should exist after Write")
	}

	data, err := svc.Read()
	if err != nil {
		t.Fatalf("Read after Write: %v", err)
	}
	if !strings.Contains(string(data), "model: text-embedding-3-small") {
		t.Errorf("Read did not round-trip the model:\n%s", data)
	}
}

// TestResolveEditor_Precedence pins $EDITOR > $VISUAL > platform default. Env
// vars are set on the test-only HOME; t.Setenv restores them afterward.
func TestResolveEditor_Precedence(t *testing.T) {
	// $EDITOR wins over $VISUAL.
	t.Setenv("EDITOR", "code")
	t.Setenv("VISUAL", "vim")
	if got := ResolveEditor(); got != "code" {
		t.Errorf("EDITOR should take precedence: got %q", got)
	}

	// $VISUAL used when $EDITOR is unset.
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "nano")
	if got := ResolveEditor(); got != "nano" {
		t.Errorf("VISUAL should be used when EDITOR unset: got %q", got)
	}

	// Neither set → platform default (vim on Unix, "" on Windows).
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	switch runtime.GOOS {
	case "windows":
		if got := ResolveEditor(); got != "" {
			t.Errorf("Windows platform default should be empty: got %q", got)
		}
	default:
		if got := ResolveEditor(); got != "vim" {
			t.Errorf("Unix platform default should be vim: got %q", got)
		}
	}
}

// fakeEditorScript writes a cross-platform script that creates the parent dir
// of its argument and writes a marker into it, then exits 0.
func fakeEditorScript(t *testing.T, home string) string {
	t.Helper()
	path := filepath.Join(home, "fake-editor")
	var script string
	var perm os.FileMode
	if runtime.GOOS == "windows" {
		path += ".cmd"
		script = "@echo off\nif not exist \"%~dp1\" mkdir \"%~dp1\"\necho edited > \"%~1\"\n"
		perm = 0o644
	} else {
		script = "#!/bin/sh\nmkdir -p \"$(dirname \"$1\")\"\necho edited > \"$1\"\n"
		perm = 0o755
	}
	if err := os.WriteFile(path, []byte(script), perm); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	return path
}

// TestEdit_LaunchesFakeEditor verifies Edit launches the resolved editor with
// the config path as an argument and returns after it exits.
func TestEdit_LaunchesFakeEditor(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("EDITOR", fakeEditorScript(t, tmpHome))

	svc := NewService()
	if err := svc.Edit(); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if !svc.Exists() {
		t.Fatalf("editor did not create the config file")
	}
}

// TestEdit_NoEditorResolvable verifies Edit errors clearly when no editor can
// be resolved. On Windows there is no platform default, so this path is
// reachable; on Unix ResolveEditor falls back to vim (nothing to error on).
func TestEdit_NoEditorResolvable(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	svc := NewService()
	if runtime.GOOS == "windows" {
		err := svc.Edit()
		if err == nil {
			t.Fatalf("Edit should error when no editor is resolvable on Windows")
		}
		if !strings.Contains(err.Error(), "no editor found") {
			t.Errorf("Edit error = %q, want it to mention 'no editor found'", err)
		}
	} else {
		if got := ResolveEditor(); got != "vim" {
			t.Errorf("ResolveEditor = %q, want \"vim\" on Unix", got)
		}
	}
}
