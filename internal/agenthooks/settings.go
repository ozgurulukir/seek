package agenthooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/renameio"
	"github.com/ozgurulukir/seek/internal/config"
)

// ReadSettings reads an agent settings file. A missing file is an empty
// settings map, not an error.
func ReadSettings(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, err
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return settings, nil
}

// WriteSettings atomically rewrites an agent settings file with a
// one-per-run timestamped backup, preserving the existing file's permission
// bits (new files are written 0600).
func WriteSettings(path string, settings map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), config.DefaultPrivateDirPerms); err != nil {
		return err
	}
	// Surgical safety (Jerry review #7): these files are shared with the agent
	// runtime — a bad rewrite or permission widening can break the user's
	// setup. Keep a one-per-run timestamped backup and never widen the mode:
	// an existing file keeps its permissions, a new one is written 0600.
	if err := backupHookSettings(path); err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	perm := os.FileMode(config.DefaultPrivateFilePerms)
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}
	return renameio.WriteFile(path, data, perm)
}

// hookSettingsBackedUp dedupes backups within a single seek run: one snapshot
// per settings file is enough for rollback, and hook repair touches the same
// file several times. A path is only marked after a real backup was taken —
// an absent file is not, so the first rewrite of a newly created file still
// gets its snapshot. Guarded by a mutex so a future parallel caller cannot
// hit the fatal concurrent-map read/write race (review 2026-09-17 L10).
var (
	hookSettingsBackedUpMu sync.Mutex
	hookSettingsBackedUp   = map[string]bool{}
)

func backupHookSettings(path string) error {
	hookSettingsBackedUpMu.Lock()
	backedUp := hookSettingsBackedUp[path]
	hookSettingsBackedUpMu.Unlock()
	if backedUp {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // first write; nothing exists to snapshot yet
		}
		return err
	}
	backup := path + ".bak-" + time.Now().Format("20060102-150405")
	if err := os.WriteFile(backup, data, config.DefaultPrivateFilePerms); err != nil {
		return err
	}
	hookSettingsBackedUpMu.Lock()
	hookSettingsBackedUp[path] = true
	hookSettingsBackedUpMu.Unlock()
	return nil
}
