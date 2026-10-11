package agenthooks

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTargetGetters(t *testing.T) {
	dummyPath := "/dummy/path/settings.json"
	target := Target{
		name:         "Test Tool",
		agent:        "testagent",
		event:        "TestEvent",
		context:      true,
		settingsPath: func() string { return dummyPath },
	}

	if got := target.Name(); got != "Test Tool" {
		t.Errorf("Name() = %q, want %q", got, "Test Tool")
	}
	if got := target.Agent(); got != "testagent" {
		t.Errorf("Agent() = %q, want %q", got, "testagent")
	}
	if got := target.Event(); got != "TestEvent" {
		t.Errorf("Event() = %q, want %q", got, "TestEvent")
	}
	if got := target.IsContext(); got != true {
		t.Errorf("IsContext() = %v, want true", got)
	}
	if got := target.SettingsPath(); got != dummyPath {
		t.Errorf("SettingsPath() = %q, want %q", got, dummyPath)
	}
}

func TestTargetInstalledAndCommandBinary(t *testing.T) {
	target := Target{
		name:          "Claude Code",
		agent:         "claude",
		event:         "Stop",
		timeout:       600,
		statusMessage: "Syncing seek index...",
		settingsPath:  func() string { return "/tmp/settings.json" },
	}

	installedSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":          "command",
							"command":       "/usr/local/bin/seek hooks sync --agent claude",
							"timeout":       float64(600),
							"statusMessage": "Syncing seek index...",
						},
					},
				},
			},
		},
	}

	notInstalledSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "other-tool sync",
						},
					},
				},
			},
		},
	}

	if !target.Installed(installedSettings) {
		t.Error("Installed() = false, want true for matching target settings")
	}
	if target.Installed(notInstalledSettings) {
		t.Error("Installed() = true, want false for non-matching target settings")
	}

	if bin, ok := target.CommandBinary(installedSettings); !ok || bin != "/usr/local/bin/seek" {
		t.Errorf("CommandBinary() = (%q, %v), want (%q, true)", bin, ok, "/usr/local/bin/seek")
	}
	if bin, ok := target.CommandBinary(notInstalledSettings); ok {
		t.Errorf("CommandBinary() = (%q, %v), want empty, false for non-matching settings", bin, ok)
	}
}

func TestDefaultSettingsPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("UserHomeDir not available: %v", err)
	}

	wantClaude := filepath.Join(home, ".claude", "settings.json")
	if got := claudeSettingsPath(); got != wantClaude {
		t.Errorf("claudeSettingsPath() = %q, want %q", got, wantClaude)
	}

	wantCodex := filepath.Join(home, ".codex", "hooks.json")
	if got := codexHooksPath(); got != wantCodex {
		t.Errorf("codexHooksPath() = %q, want %q", got, wantCodex)
	}
}

func TestTargetsReturnsCopy(t *testing.T) {
	targets1 := Targets()
	targets2 := Targets()

	if len(targets1) == 0 {
		t.Fatal("Targets() returned empty slice")
	}

	if len(targets1) != len(targets2) {
		t.Fatalf("Targets() len mismatch: %d vs %d", len(targets1), len(targets2))
	}

	for i := range targets1 {
		if targets1[i].Name() != targets2[i].Name() ||
			targets1[i].Agent() != targets2[i].Agent() ||
			targets1[i].Event() != targets2[i].Event() ||
			targets1[i].IsContext() != targets2[i].IsContext() ||
			targets1[i].SettingsPath() != targets2[i].SettingsPath() {
			t.Errorf("Targets()[%d] mismatch: %#v vs %#v", i, targets1[i], targets2[i])
		}
	}

	// Modify returned slice copy
	targets1[0].name = "Mutated Tool Name"

	targets3 := Targets()
	if targets3[0].name == "Mutated Tool Name" {
		t.Error("Targets() modified internal hookTargets registry; expected copy")
	}
}

func TestSelectedFiltering(t *testing.T) {
	all := Selected(false, false)
	if len(all) != len(hookTargets) {
		t.Errorf("Selected(false, false) len = %d, want %d", len(all), len(hookTargets))
	}

	both := Selected(true, true)
	if len(both) != len(hookTargets) {
		t.Errorf("Selected(true, true) len = %d, want %d", len(both), len(hookTargets))
	}

	claudeOnly := Selected(true, false)
	for _, target := range claudeOnly {
		if target.Name() != "Claude Code" {
			t.Errorf("Selected(true, false) contained target with name %q, want Claude Code", target.Name())
		}
	}

	codexOnly := Selected(false, true)
	for _, target := range codexOnly {
		if target.Name() != "Codex" {
			t.Errorf("Selected(false, true) contained target with name %q, want Codex", target.Name())
		}
	}
}

func TestHookEvents(t *testing.T) {
	events := hookEvents()
	expected := []string{"Stop", "UserPromptSubmit", "Interrupt"}

	if !reflect.DeepEqual(events, expected) {
		t.Errorf("hookEvents() = %v, want %v", events, expected)
	}
}
