package cmd

import (
	"os"
	"path/filepath"
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

func TestRemoveSeekEntriesFrom_KeepsOtherTools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	content := `{"hooks": {"Stop": [
		{"hooks": [{"type": "command", "command": "other-tool run"}]},
		{"hooks": [{"type": "command", "command": "'seek' hooks sync"}]}
	]}}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeSeekEntriesFrom(path); err != nil {
		t.Fatalf("removeSeekEntriesFrom: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !contains(string(data), "other-tool run") {
		t.Errorf("other tool's entry was removed: %s", data)
	}
	if contains(string(data), "seek") {
		t.Errorf("seek entry still present: %s", data)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
