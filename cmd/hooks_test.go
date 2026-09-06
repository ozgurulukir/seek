package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTestTarget(t *testing.T, name string) hookTarget {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	return hookTarget{
		name:         name,
		settingsPath: func() string { return path },
		event:        "Stop",
	}
}

func newCodexTestTarget(t *testing.T) hookTarget {
	tgt := newTestTarget(t, "Codex")
	tgt.codexOutput = true
	return tgt
}

func readTestSettings(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	return m
}

func TestInstallHook_CreatesSettings(t *testing.T) {
	tgt := newTestTarget(t, "Test Tool")

	if err := installHook(tgt); err != nil {
		t.Fatalf("installHook: %v", err)
	}

	settings := readTestSettings(t, tgt.settingsPath())
	if findSeekHookIndex(settings, "Stop") < 0 {
		t.Fatal("seek hook not found after install")
	}

	hooks := settings["hooks"].(map[string]interface{})
	stop := hooks["Stop"].([]interface{})
	entry := stop[0].(map[string]interface{})
	hookList := entry["hooks"].([]interface{})
	cmd := hookList[0].(map[string]interface{})
	if cmd["type"] != "command" {
		t.Errorf("hook type = %v, want command", cmd["type"])
	}
	if got := cmd["command"].(string); got == "" || got == "sync" {
		t.Errorf("unexpected hook command: %q", got)
	}
}

func TestInstallHook_Idempotent(t *testing.T) {
	tgt := newTestTarget(t, "Test Tool")

	if err := installHook(tgt); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := installHook(tgt); err != nil {
		t.Fatalf("second install: %v", err)
	}

	settings := readTestSettings(t, tgt.settingsPath())
	hooks := settings["hooks"].(map[string]interface{})
	stop := hooks["Stop"].([]interface{})
	if len(stop) != 1 {
		t.Fatalf("expected 1 Stop entry after duplicate install, got %d", len(stop))
	}
}

func TestCodexHookReturnsJSONAfterSync(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by platform-specific command generation")
	}

	command := hookCommand(hookTarget{codexOutput: true}, "false")
	output, err := exec.Command("sh", "-c", command).Output()
	if err != nil {
		t.Fatalf("run Codex hook wrapper: %v", err)
	}
	if !json.Valid(output) {
		t.Fatalf("Codex hook output is not JSON: %q", output)
	}
	if string(output) != "{}\n" {
		t.Errorf("Codex hook output = %q, want empty JSON object", output)
	}
}

func TestInstallHook_UpgradesLegacyCodexCommand(t *testing.T) {
	tgt := newCodexTestTarget(t)
	legacy := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{map[string]interface{}{
				"matcher": "",
				"hooks":   []interface{}{map[string]interface{}{"type": "command", "command": "seek sync"}},
			}},
		},
	}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(tgt.settingsPath(), data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := installHook(tgt); err != nil {
		t.Fatalf("installHook: %v", err)
	}

	settings := readTestSettings(t, tgt.settingsPath())
	hooks := settings["hooks"].(map[string]interface{})
	stop := hooks["Stop"].([]interface{})
	if len(stop) != 1 {
		t.Fatalf("legacy hook should be upgraded in place, got %d entries", len(stop))
	}
	command := stop[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})["command"].(string)
	if command == "seek sync" || !strings.Contains(command, "seek sync") {
		t.Errorf("Codex hook command = %q, want JSON wrapper around seek sync", command)
	}
}

func TestInstallHook_PreservesExistingHooks(t *testing.T) {
	tgt := newTestTarget(t, "Test Tool")
	path := tgt.settingsPath()

	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "echo hi"},
					},
				},
			},
			"SessionStart": []interface{}{
				map[string]interface{}{
					"matcher": "startup",
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "echo start"},
					},
				},
			},
		},
		"otherKey": "keepme",
	}
	data, _ := json.Marshal(existing)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := installHook(tgt); err != nil {
		t.Fatalf("installHook: %v", err)
	}

	settings := readTestSettings(t, path)
	if settings["otherKey"] != "keepme" {
		t.Error("unrelated top-level key lost")
	}
	hooks := settings["hooks"].(map[string]interface{})
	if len(hooks["Stop"].([]interface{})) != 2 {
		t.Error("existing Stop hook not preserved")
	}
	if _, ok := hooks["SessionStart"]; !ok {
		t.Error("SessionStart event lost")
	}
}

func TestUninstallHook_RemovesOnlySeek(t *testing.T) {
	tgt := newTestTarget(t, "Test Tool")
	if err := installHook(tgt); err != nil {
		t.Fatalf("installHook: %v", err)
	}

	// Add a foreign Stop hook alongside the seek hook.
	settings := readTestSettings(t, tgt.settingsPath())
	hooks := settings["hooks"].(map[string]interface{})
	stop := hooks["Stop"].([]interface{})
	other := map[string]interface{}{
		"matcher": "",
		"hooks": []interface{}{
			map[string]interface{}{"type": "command", "command": "some-other-tool run"},
		},
	}
	hooks["Stop"] = append(stop, other)
	if err := writeHookSettings(tgt.settingsPath(), settings); err != nil {
		t.Fatal(err)
	}

	if err := uninstallHook(tgt); err != nil {
		t.Fatalf("uninstallHook: %v", err)
	}

	settings = readTestSettings(t, tgt.settingsPath())
	hooks = settings["hooks"].(map[string]interface{})
	stop = hooks["Stop"].([]interface{})
	if len(stop) != 1 {
		t.Fatalf("expected foreign hook to remain, got %d entries", len(stop))
	}
	if findSeekHookIndex(settings, "Stop") >= 0 {
		t.Error("seek hook still present after uninstall")
	}
}

func TestUninstallHook_NotInstalled(t *testing.T) {
	tgt := newTestTarget(t, "Test Tool")
	// Should be a no-op without error on a missing file.
	if err := uninstallHook(tgt); err != nil {
		t.Fatalf("uninstallHook on missing file: %v", err)
	}
}

func TestUninstallHook_CleansEmptyEvent(t *testing.T) {
	tgt := newTestTarget(t, "Test Tool")
	if err := installHook(tgt); err != nil {
		t.Fatalf("installHook: %v", err)
	}
	if err := uninstallHook(tgt); err != nil {
		t.Fatalf("uninstallHook: %v", err)
	}
	settings := readTestSettings(t, tgt.settingsPath())
	hooks := settings["hooks"].(map[string]interface{})
	if _, ok := hooks["Stop"]; ok {
		t.Error("empty Stop event should be deleted")
	}
}

func TestSelectedTargets(t *testing.T) {
	all := selectedTargets(false, false)
	if len(all) != len(hookTargets) {
		t.Errorf("no flags: got %d targets, want %d", len(all), len(hookTargets))
	}
	claudeOnly := selectedTargets(true, false)
	if len(claudeOnly) != 1 || claudeOnly[0].name != "Claude Code" {
		t.Errorf("--claude: got %v", claudeOnly)
	}
	codexOnly := selectedTargets(false, true)
	if len(codexOnly) != 1 || codexOnly[0].name != "Codex" {
		t.Errorf("--codex: got %v", codexOnly)
	}
}
