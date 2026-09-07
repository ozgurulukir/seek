package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func newClaudeFixtureTarget(t *testing.T) hookTarget {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", "settings.json")
	return hookTarget{
		name:          "Claude Code",
		settingsPath:  func() string { return path },
		event:         "Stop",
		timeout:       60,
		statusMessage: "Syncing seek index...",
	}
}

func newCodexFixtureTarget(t *testing.T) hookTarget {
	dir := t.TempDir()
	path := filepath.Join(dir, ".codex", "hooks.json")
	return hookTarget{
		name:         "Codex",
		settingsPath: func() string { return path },
		event:        "Stop",
		codexOutput:  true,
	}
}

func copyHookFixture(t *testing.T, name, destination string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hooks", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(destination, data, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
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

func TestInstallHook_ClaudeFixtureIncludesStatusAndTimeout(t *testing.T) {
	tgt := newClaudeFixtureTarget(t)
	if err := installHook(tgt); err != nil {
		t.Fatalf("installHook: %v", err)
	}

	settings := readTestSettings(t, tgt.settingsPath())
	command := settings["hooks"].(map[string]interface{})["Stop"].([]interface{})[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	if got := command["timeout"]; got != float64(60) {
		t.Errorf("timeout = %v, want 60", got)
	}
	if got := command["statusMessage"]; got != "Syncing seek index..." {
		t.Errorf("statusMessage = %v", got)
	}
	if got := command["command"].(string); !strings.HasSuffix(got, " sync") || strings.Contains(got, " hooks sync") {
		t.Errorf("Claude command = %q, want direct seek sync", got)
	}
}

func TestInstallHook_UpgradesExistingClaudeFixture(t *testing.T) {
	tgt := newClaudeFixtureTarget(t)
	copyHookFixture(t, "claude-settings.json", tgt.settingsPath())

	if err := installHook(tgt); err != nil {
		t.Fatalf("installHook: %v", err)
	}

	settings := readTestSettings(t, tgt.settingsPath())
	command := settings["hooks"].(map[string]interface{})["Stop"].([]interface{})[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	if got := command["timeout"]; got != float64(60) {
		t.Errorf("timeout = %v, want 60", got)
	}
	if got := command["statusMessage"]; got != "Syncing seek index..." {
		t.Errorf("statusMessage = %v", got)
	}
	if settings["permissions"].(map[string]interface{})["defaultMode"] != "acceptEdits" {
		t.Error("unrelated Claude setting was not preserved")
	}
	if _, ok := settings["hooks"].(map[string]interface{})["SessionStart"]; !ok {
		t.Error("unrelated Claude hook was not preserved")
	}
}

func TestInstallHook_CodexFixtureUsesJSONOnlyCommand(t *testing.T) {
	tgt := newCodexFixtureTarget(t)
	copyHookFixture(t, "codex-hooks.json", tgt.settingsPath())
	if err := installHook(tgt); err != nil {
		t.Fatalf("installHook: %v", err)
	}

	settings := readTestSettings(t, tgt.settingsPath())
	command := settings["hooks"].(map[string]interface{})["Stop"].([]interface{})[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	if got := command["command"].(string); !strings.Contains(got, " hooks sync") {
		t.Errorf("Codex command = %q, want seek hooks sync", got)
	}
	if _, exists := command["timeout"]; exists {
		t.Error("Codex command unexpectedly has a Claude-specific timeout")
	}
	if _, exists := command["statusMessage"]; exists {
		t.Error("Codex command unexpectedly has a Claude-specific status message")
	}
	if _, ok := settings["hooks"].(map[string]interface{})["PreToolUse"]; !ok {
		t.Error("unrelated Codex hook was not preserved")
	}
}

func TestSeekHookCommandDoesNotMatchOtherCommands(t *testing.T) {
	for _, command := range []string{
		"/opt/seek-helper sync",
		"other-seek-tool sync",
		"/opt/seek-tools/sync",
		"echo 'seek sync'",
		"sh -c 'echo seek sync'",
		"other-tool && seek sync",
	} {
		if isSeekHookCommand(command) {
			t.Errorf("unexpected match for %q", command)
		}
	}
}

func TestSeekHookCommandMatchesQuotedPaths(t *testing.T) {
	for _, command := range []string{
		"'/Applications/Seek Tools/seek' sync",
		"'/Applications/Seek Tools/seek' hooks sync",
		`"C:\Program Files\Seek\seek.exe" sync`,
		`"C:\Program Files\Seek\seek.exe" hooks sync`,
		`sh -c "/Applications/Seek Tools/seek sync >/dev/null 2>&1; printf '{}\\n'"`,
	} {
		if !isSeekHookCommand(command) {
			t.Errorf("expected match for %q", command)
		}
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

func TestRunHooksSyncReturnsJSONAfterFailedSync(t *testing.T) {
	var output bytes.Buffer
	err := runHooksSync(func() error {
		return errors.New("sync failed")
	}, &output)
	if err != nil {
		t.Fatalf("runHooksSync: %v", err)
	}

	outputBytes := output.Bytes()
	if !json.Valid(outputBytes) {
		t.Fatalf("Codex hook output is not JSON: %q", outputBytes)
	}
	if got := output.String(); got != "{}\n" {
		t.Errorf("Codex hook output = %q, want empty JSON object", got)
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
	if !strings.Contains(command, " hooks sync") {
		t.Errorf("Codex hook command = %q, want JSON-only seek hooks sync command", command)
	}
	if strings.Contains(command, "sh -c") || strings.Contains(command, "cmd /C") {
		t.Errorf("Codex hook command should not depend on a shell: %q", command)
	}
}

func TestCommandLineQuotesBinaryPath(t *testing.T) {
	tests := []struct {
		name, goos, binary, want string
	}{
		{"POSIX", "linux", "/path with spaces/seek", "'/path with spaces/seek' hooks sync"},
		{"Windows", "windows", `C:\Program Files\Seek\seek.exe`, `"C:\Program Files\Seek\seek.exe" hooks sync`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandLineForOS(tt.goos, tt.binary, "hooks", "sync"); got != tt.want {
				t.Errorf("command = %q, want %q", got, tt.want)
			}
		})
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

func TestUninstallHook_PreservesOtherCommandsInSameEntry(t *testing.T) {
	tgt := newTestTarget(t, "Test Tool")
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{map[string]interface{}{
				"matcher": "",
				"hooks": []interface{}{
					map[string]interface{}{"type": "command", "command": "seek sync"},
					map[string]interface{}{"type": "command", "command": "echo preserve-me"},
				},
			}},
		},
	}
	if err := writeHookSettings(tgt.settingsPath(), settings); err != nil {
		t.Fatal(err)
	}

	if err := uninstallHook(tgt); err != nil {
		t.Fatalf("uninstallHook: %v", err)
	}

	settings = readTestSettings(t, tgt.settingsPath())
	stop := settings["hooks"].(map[string]interface{})["Stop"].([]interface{})
	hooks := stop[0].(map[string]interface{})["hooks"].([]interface{})
	if len(hooks) != 1 || hooks[0].(map[string]interface{})["command"] != "echo preserve-me" {
		t.Errorf("unrelated command was not preserved: %#v", hooks)
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
