package agenthooks

import (
	"os"
	"path/filepath"
	"testing"
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
