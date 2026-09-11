package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/ozgurulukir/seek/internal/agenthooks"
	"github.com/ozgurulukir/seek/internal/config"
)

// UninstallCmd removes seek from the machine, one artifact class at a time:
// the periodic service, agent hooks, the cache/index, and the config. Run
// with --dry-run first: it lists every artifact without changing anything.
//
// The binary itself is never deleted from within seek (a running binary
// cannot reliably unlink itself on Windows); the uninstall summary says so.
type UninstallCmd struct {
	DryRun  bool `help:"List everything that would be removed, change nothing."`
	Hooks   bool `help:"Remove agent hooks (Claude/Codex)."`
	Service bool `help:"Stop and remove the periodic sync service."`
	Cache   bool `help:"Delete the cache/index directory (contains all searchable text)."`
	Config  bool `help:"Delete the config directory (~/.config/seek)."`
}

type uninstallArtifact struct {
	class string // service | hooks | cache | config
	path  string
	extra string // human note
}

// uninstallArtifacts enumerates what seek owns on this machine, filtered by
// the selected classes (no class flags = all classes).
func uninstallArtifacts(cfg *config.AppConfig, service, hooks, cache, configDirSel bool) []uninstallArtifact {
	want := func(class string) bool {
		if !service && !hooks && !cache && !configDirSel {
			return true
		}
		switch class {
		case "service":
			return service
		case "hooks":
			return hooks
		case "cache":
			return cache
		case "config":
			return configDirSel
		}
		return false
	}

	var out []uninstallArtifact
	if want("service") {
		switch runtime.GOOS {
		case "darwin":
			out = append(out, uninstallArtifact{class: "service", path: plistPath(), extra: "launchd LaunchAgent"})
		case "linux":
			out = append(out,
				uninstallArtifact{class: "service", path: filepath.Join(systemdDir(), "seek.timer"), extra: "systemd user unit"},
				uninstallArtifact{class: "service", path: filepath.Join(systemdDir(), "seek.service"), extra: "systemd user unit"},
			)
		case "windows":
			out = append(out, uninstallArtifact{class: "service", path: windowsTask, extra: "Task Scheduler task (schtasks /Delete)"})
		}
	}
	if want("hooks") {
		seen := map[string]bool{}
		for _, t := range agenthooks.Targets() {
			p := t.SettingsPath()
			if seen[p] {
				continue // both Claude events share one settings file
			}
			seen[p] = true
			out = append(out, uninstallArtifact{class: "hooks", path: p, extra: "seek entries"})
		}
	}
	if want("cache") {
		out = append(out,
			uninstallArtifact{class: "cache", path: cfg.CacheDir, extra: "index + embeddings cache (contains all searchable text)"},
			uninstallArtifact{class: "cache", path: filepath.Join(config.CacheDir(), "hnsw.index"), extra: "HNSW vector index (default path)"},
		)
	}
	if want("config") {
		out = append(out, uninstallArtifact{class: "config", path: config.ConfigDir(), extra: "config.yaml + hook state"})
	}
	return out
}

func (c *UninstallCmd) Run(cfg *config.AppConfig) error {
	arts := uninstallArtifacts(cfg, c.Service, c.Hooks, c.Cache, c.Config)
	if len(arts) == 0 {
		fmt.Println("Nothing to remove for the selected classes.")
		return nil
	}

	fmt.Println("Would remove:")
	for _, a := range arts {
		// Windows: the service artifact is a Task Scheduler task with no file
		// path, so stat it never; everything else is a real file/dir.
		exists := true
		if runtime.GOOS != "windows" || a.class != "service" {
			if _, err := os.Stat(a.path); err != nil {
				exists = false
			}
		}
		note := a.extra
		if !exists {
			note += " [not present]"
		}
		fmt.Printf("  [%s] %s  (%s)\n", a.class, a.path, note)
	}

	if c.DryRun {
		fmt.Println("\ndry run: nothing was changed. Re-run with the class flags (or none for all) to remove.")
		return nil
	}

	for _, a := range arts {
		switch a.class {
		case "service":
			switch runtime.GOOS {
			case "windows":
				if out, err := exec.Command("schtasks", "/Delete", "/F", "/TN", windowsTask).CombinedOutput(); err != nil {
					fmt.Printf("  WARN: schtasks delete: %s\n", string(out))
					continue
				}
			case "darwin":
				if out, err := runLaunchctl("bootout", fmt.Sprintf("gui/%d", os.Getuid()), a.path); err != nil {
					fmt.Printf("  WARN: launchctl bootout: %s\n", string(out))
				}
			case "linux":
				_ = exec.Command("systemctl", "--user", "disable", "--now", "seek.timer").Run()
				_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
			}
			if runtime.GOOS != "windows" {
				if err := os.Remove(a.path); err != nil && !os.IsNotExist(err) {
					fmt.Printf("  WARN: remove %s: %v\n", a.path, err)
					continue
				}
			}
			fmt.Printf("  removed: %s\n", a.path)
		case "hooks":
			// Delegate to the surgical hook remover: it only touches seek's
			// own entries and keeps other tools' configuration intact.
			if err := agenthooks.RemoveAllSeekEntries(a.path); err != nil {
				fmt.Printf("  WARN: hooks %s: %v\n", a.path, err)
				continue
			}
			fmt.Printf("  cleaned seek entries from: %s\n", a.path)
		case "cache", "config":
			if err := os.RemoveAll(a.path); err != nil {
				fmt.Printf("  WARN: remove %s: %v\n", a.path, err)
				continue
			}
			fmt.Printf("  removed: %s\n", a.path)
		}
	}
	fmt.Println("\nuninstall complete. The binary itself was not deleted (a running binary cannot safely delete itself); remove it manually.")
	return nil
}
