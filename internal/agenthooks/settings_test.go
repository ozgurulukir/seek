package agenthooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadSettings_MissingFileIsEmptyMap(t *testing.T) {
	settings, err := ReadSettings(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("ReadSettings on missing file: %v", err)
	}
	if settings == nil {
		t.Fatal("ReadSettings on missing file = nil map, want empty map")
	}
	// The map must be writable — hook install assigns into it.
	settings["hooks"] = map[string]interface{}{}
}

// A hand-edited settings file containing the literal JSON null must not hand
// back a nil map (which panics on first assignment during hook install).
func TestReadSettings_NullFileIsEmptyMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := ReadSettings(path)
	if err != nil {
		t.Fatalf("ReadSettings on null file: %v", err)
	}
	if settings == nil {
		t.Fatal("ReadSettings on null file = nil map, want empty map")
	}
	settings["hooks"] = map[string]interface{}{}
}

func TestReadSettings_InvalidJSONIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSettings(path); err == nil {
		t.Fatal("ReadSettings on invalid JSON: expected error, got nil")
	}
}

func TestHookSettingsBackupPath_CollisionResistant(t *testing.T) {
	path := "/home/u/.claude/settings.json"
	now := time.Date(2026, 11, 24, 10, 30, 15, 123456789, time.UTC)

	a := hookSettingsBackupPath(path, now, 111)
	b := hookSettingsBackupPath(path, now, 222)
	if a == b {
		t.Fatalf("different PIDs produced the same backup name: %q", a)
	}
	c := hookSettingsBackupPath(path, now.Add(time.Nanosecond), 111)
	if a == c {
		t.Fatalf("different nanosecond timestamps produced the same backup name: %q", a)
	}
	if want := path + ".bak-20261124-103015.123456789-111"; a != want {
		t.Errorf("hookSettingsBackupPath = %q, want %q", a, want)
	}
}

func TestPruneHookSettingsBackups_KeepsNewest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	var names []string
	for i := 0; i < hookSettingsBackupKeep+3; i++ {
		name := hookSettingsBackupPath(path, now.Add(time.Duration(i)*time.Second), 100+i)
		if err := os.WriteFile(name, []byte("backup"), 0600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	pruneHookSettingsBackups(path)

	backups, err := filepath.Glob(path + ".bak-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != hookSettingsBackupKeep {
		t.Fatalf("got %d backups after prune, want %d: %v", len(backups), hookSettingsBackupKeep, backups)
	}
	for _, name := range names[len(names)-hookSettingsBackupKeep:] {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("newest backup %q was pruned: %v", name, err)
		}
	}
}
