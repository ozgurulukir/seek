package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
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
	return newTestTarget(t, "Codex")
}

func newClaudeFixtureTarget(t *testing.T) hookTarget {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", "settings.json")
	return hookTarget{
		name:          "Claude Code",
		agent:         "claude",
		settingsPath:  func() string { return path },
		event:         "Stop",
		timeout:       600,
		statusMessage: "Syncing seek index...",
	}
}

func newCodexFixtureTarget(t *testing.T) hookTarget {
	dir := t.TempDir()
	path := filepath.Join(dir, ".codex", "hooks.json")
	return hookTarget{
		name:         "Codex",
		agent:        "codex",
		settingsPath: func() string { return path },
		event:        "Stop",
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
	if got := command["timeout"]; got != float64(600) {
		t.Errorf("timeout = %v, want 600", got)
	}
	if got := command["statusMessage"]; got != "Syncing seek index..." {
		t.Errorf("statusMessage = %v", got)
	}
	if got := command["command"].(string); !strings.Contains(got, " hooks sync --agent claude") {
		t.Errorf("Claude command = %q, want agent-scoped seek hooks sync", got)
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
	if got := command["timeout"]; got != float64(600) {
		t.Errorf("timeout = %v, want 600", got)
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
		`"C:\Program Files\Seek\seek.exe" hooks sync --agent codex --embed`,
		`"C:\Program Files\Seek\seek.exe" hooks context --agent codex`,
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

func TestInstallHook_PreservesEmbedOnRepair(t *testing.T) {
	tgt := newClaudeFixtureTarget(t)
	tgt.embed = true
	if err := installHook(tgt); err != nil {
		t.Fatalf("install embedded hook: %v", err)
	}
	tgt.embed = false
	if err := installHook(tgt); err != nil {
		t.Fatalf("repair hook: %v", err)
	}
	settings := readTestSettings(t, tgt.settingsPath())
	command := settings["hooks"].(map[string]interface{})["Stop"].([]interface{})[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})["command"].(string)
	if !strings.Contains(command, "--embed") {
		t.Errorf("repair removed --embed from %q", command)
	}
}

func TestTargetHookDoesNotAcceptLegacyCommand(t *testing.T) {
	target := newCodexFixtureTarget(t)
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{map[string]interface{}{
				"hooks": []interface{}{map[string]interface{}{"command": "seek sync"}},
			}},
		},
	}
	if findSeekHookIndex(settings, target.event) < 0 {
		t.Fatal("legacy hook must remain discoverable for upgrade")
	}
	if findTargetSeekHookIndex(settings, target) >= 0 {
		t.Fatal("legacy hook must not be reported as a current target hook")
	}
}

func TestAcquireHookLockDoesNotReclaimOldLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks", "codex.lock")
	lock, err := acquireHookLock(context.Background(), path)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("age lock: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := acquireHookLock(ctx, path); err == nil {
		t.Fatal("active old lock was acquired")
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if replacement, err := acquireHookLock(context.Background(), path); err != nil {
		t.Fatalf("acquire released lock: %v", err)
	} else if err := replacement.Close(); err != nil {
		t.Fatalf("release replacement lock: %v", err)
	}
}

func TestHookSyncLockIsSharedAcrossAgents(t *testing.T) {
	cfg := &config.AppConfig{CacheDir: t.TempDir()}
	if got, want := hookSyncLockPath(cfg), filepath.Join(cfg.CacheDir, "hooks", "sync.lock"); got != want {
		t.Errorf("lock path = %q, want %q", got, want)
	}
}

func TestHookCommandBinary(t *testing.T) {
	target := newClaudeFixtureTarget(t)
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{map[string]interface{}{
				"hooks": []interface{}{map[string]interface{}{"command": `"C:\Program Files\Seek\seek.exe" hooks sync --agent claude`}},
			}},
		},
	}
	if got, ok := hookCommandBinary(settings, target); !ok || got != `C:\Program Files\Seek\seek.exe` {
		t.Errorf("hookCommandBinary = (%q, %t)", got, ok)
	}
}

func TestHookPrompt(t *testing.T) {
	if got := hookPrompt([]byte(`{"prompt":"find this"}`)); got != "find this" {
		t.Errorf("prompt = %q", got)
	}
	if got := hookPrompt([]byte(`not json`)); got != "" {
		t.Errorf("invalid prompt = %q, want empty", got)
	}
}

func TestHookSearchArgsScopesAgent(t *testing.T) {
	args := hookSearchArgs("claude", "find this")
	if got, want := strings.Join(args, " "), "search find this --lex -l 3 --doc-type claude"; got != want {
		t.Errorf("search args = %q, want %q", got, want)
	}
}

func TestHookContextResponseUsesSpecificOutputEnvelope(t *testing.T) {
	response := hookContextResponse("relevant context")
	if _, ok := response["additionalContext"]; ok {
		t.Fatalf("unexpected top-level additionalContext: %#v", response["additionalContext"])
	}
	specific, ok := response["hookSpecificOutput"].(map[string]string)
	if !ok || specific["hookEventName"] != "UserPromptSubmit" || specific["additionalContext"] != "relevant context" {
		t.Errorf("hook-specific response = %#v", response["hookSpecificOutput"])
	}
}

func TestHookRuntimeBinaryPreservesAbsoluteArgv0(t *testing.T) {
	if got := hookRuntimeBinaryFor(`/opt/seek/bin/seek`, "seek"); got != `/opt/seek/bin/seek` {
		t.Errorf("runtime binary = %q", got)
	}
	if got := hookRuntimeBinaryFor("seek", "/opt/seek/bin/seek"); got != "/opt/seek/bin/seek" {
		t.Errorf("PATH fallback binary = %q", got)
	}
}

func TestWithHookLockEnvReplacesExistingValue(t *testing.T) {
	env := withHookLockEnv([]string{"PATH=/bin", hookLockEnv + "=0"})
	count := 0
	for _, value := range env {
		if strings.HasPrefix(value, hookLockEnv+"=") {
			count++
			if value != hookLockEnv+"=1" {
				t.Errorf("lock env = %q", value)
			}
		}
	}
	if count != 1 {
		t.Errorf("lock env entries = %d, want 1", count)
	}
}

func TestRecordHookSkip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.json")
	completed := time.Now().Add(-time.Minute).Round(0)
	if err := writeHookState(path, hookState{Agent: "claude", CompletedAt: completed}); err != nil {
		t.Fatalf("write state: %v", err)
	}
	recordHookSkip(path, hookState{Agent: "claude", CompletedAt: completed}, "debounced")
	state, ok := readHookState(path)
	if !ok || state.SkippedReason != "debounced" || !state.LastAttemptAt.After(completed) {
		t.Errorf("skip state = %#v, ok=%t", state, ok)
	}
}

func TestLimitedWriterCapsRetainedOutput(t *testing.T) {
	var output bytes.Buffer
	writer := &limitedWriter{writer: &output, remaining: 3}
	if n, err := writer.Write([]byte("abcdef")); err != nil || n != 6 {
		t.Fatalf("Write = (%d, %v), want (6, nil)", n, err)
	}
	if got := output.String(); got != "abc" {
		t.Errorf("output = %q, want %q", got, "abc")
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
	if len(claudeOnly) != 2 || claudeOnly[0].name != "Claude Code" || claudeOnly[1].event != "UserPromptSubmit" {
		t.Errorf("--claude: got %v", claudeOnly)
	}
	codexOnly := selectedTargets(false, true)
	if len(codexOnly) != 2 || codexOnly[0].name != "Codex" || codexOnly[1].event != "UserPromptSubmit" {
		t.Errorf("--codex: got %v", codexOnly)
	}
}

func TestIsSeekHookCommand_MatcherMatrix(t *testing.T) {
	mustMatch := []string{
		// bare, and the quoted bare form install writes when LookPath fails (CI)
		"seek hooks sync",
		"seek sync",
		"seek.exe hooks sync",
		"'seek' hooks sync",
		`"seek" hooks sync`,
		"'seek' context --agent claude",
		// absolute POSIX and Windows paths
		"'/home/u/go/bin/seek' hooks sync",
		"'/opt/seek' sync",
		`"C:\\tools\\seek.exe" hooks sync`,
	}
	mustNotMatch := []string{
		// lookalike binaries must never match: uninstall deletes by this matcher
		"'myseek' hooks sync",
		`"myseek" hooks sync`,
		"myseek hooks sync",
		"'seekery' hooks sync",
		"seek-other hooks sync",
		"'other' hooks sync",
		// wrong/incomplete invocations
		"/x/seek extra thing",
		"'seek' hooks",
	}
	for _, c := range mustMatch {
		if !isSeekHookCommand(c) {
			t.Errorf("isSeekHookCommand(%q) = false, want true", c)
		}
	}
	for _, c := range mustNotMatch {
		if isSeekHookCommand(c) {
			t.Errorf("isSeekHookCommand(%q) = true, want false", c)
		}
	}
}

func TestWriteHookSettings_BackupAndPerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	// Fresh write: no backup, file created 0600.
	settings := map[string]interface{}{"hooks": map[string]interface{}{}}
	if err := writeHookSettings(path, settings); err != nil {
		t.Fatalf("fresh write: %v", err)
	}
	entries, _ := filepath.Glob(path + ".bak-*")
	if len(entries) != 0 {
		t.Errorf("backup created on first write: %v", entries)
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if got := fi.Mode().Perm(); got != 0600 {
		t.Errorf("new settings perm = %04o, want 0600", uint32(got))
	}

	// Existing 0644 file: must be backed up once, and its 0644 mode preserved
	// (repair must never widen OR silently tighten the user's chosen mode).
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeHookSettings(path, map[string]interface{}{"a": 1}); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	baks, _ := filepath.Glob(path + ".bak-*")
	if len(baks) != 1 {
		t.Fatalf("got %d backups, want 1 (dedup per run): %v", len(baks), baks)
	}
	data, err := os.ReadFile(baks[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"hooks"`) {
		t.Errorf("backup does not contain the pre-rewrite content: %q", data)
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if got := fi.Mode().Perm(); got != 0644 {
		t.Errorf("existing settings perm = %04o, want preserved 0644", uint32(got))
	}
}
