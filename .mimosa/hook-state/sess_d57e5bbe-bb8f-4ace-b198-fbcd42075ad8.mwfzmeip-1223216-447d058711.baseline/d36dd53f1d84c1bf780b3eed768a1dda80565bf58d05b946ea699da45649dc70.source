package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ozgurulukir/seek/internal/config"
)

func TestUninstallArtifacts_Classes(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	cfg := &config.AppConfig{CacheDir: filepath.Join(tmpHome, ".cache", "seek")}

	all := uninstallArtifacts(cfg, false, false, false, false)
	if len(all) == 0 {
		t.Fatal("no artifacts enumerated")
	}
	// Only hooks selected: hooks settings paths, nothing else.
	hooksOnly := uninstallArtifacts(cfg, false, true, false, false)
	if len(hooksOnly) != 2 {
		t.Errorf("hooks-only got %d artifacts (%v), want 2 (claude+codex)", len(hooksOnly), hooksOnly)
	}
	for _, a := range hooksOnly {
		if a.class != "hooks" {
			t.Errorf("class = %q, want hooks", a.class)
		}
	}
	// Cache + config only: cache dir and config dir.
	cc := uninstallArtifacts(cfg, false, false, true, true)
	if len(cc) < 2 {
		t.Errorf("cache+config got %d, want >= 2", len(cc))
	}
}

func TestUninstall_DryRunChangesNothing(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	cfgDir := filepath.Join(tmpHome, ".config", "seek")
	cacheDir := filepath.Join(tmpHome, ".cache", "seek")
	for _, dir := range []string{cfgDir, cacheDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.AppConfig{CacheDir: cacheDir}

	cmd := &UninstallCmd{DryRun: true}
	if err := cmd.Run(cfg); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if _, err := os.Stat(cfgDir); err != nil {
		t.Error("config dir removed by dry run")
	}
	if _, err := os.Stat(cacheDir); err != nil {
		t.Error("cache dir removed by dry run")
	}
}

func TestUninstall_RemovesAllClasses(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	cfgDir := filepath.Join(tmpHome, ".config", "seek")
	cacheDir := filepath.Join(tmpHome, ".cache", "seek")
	for _, dir := range []string{cfgDir, cacheDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.AppConfig{CacheDir: cacheDir}

	cmd := &UninstallCmd{}
	if err := cmd.Run(cfg); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(cfgDir); !os.IsNotExist(err) {
		t.Errorf("config dir still exists after uninstall: %v", err)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Errorf("cache dir still exists after uninstall: %v", err)
	}
}

func TestUninstall_DryRunMarksNotPresent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	cfg := &config.AppConfig{CacheDir: filepath.Join(tmpHome, ".cache", "missing")}
	cmd := &UninstallCmd{DryRun: true}
	// No panic; missing cache dir is listed with [not present] on non-Windows.
	if runtime.GOOS != "windows" {
		if err := cmd.Run(cfg); err != nil {
			t.Fatalf("dry run on missing dirs: %v", err)
		}
	}
}
