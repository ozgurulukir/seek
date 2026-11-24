package agenthooks

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRemoveAllSeekEntries_KeepsOtherTools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	content := `{"hooks": {"Stop": [
		{"hooks": [{"type": "command", "command": "other-tool run"}]},
		{"hooks": [{"type": "command", "command": "'seek' hooks sync"}]}
	]}}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAllSeekEntries(path); err != nil {
		t.Fatalf("RemoveAllSeekEntries: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "other-tool run") {
		t.Errorf("other tool's entry was removed: %s", data)
	}
	if strings.Contains(string(data), "seek") {
		t.Errorf("seek entry still present: %s", data)
	}
}

// TestHookCommandGolden pins the exact command strings install writes into
// user settings files. Installed agents are matched byte-for-byte by the
// matcher regexes, so any change here orphans existing hooks on upgrade.
//
// The pinned bytes are platform-aware: install quotes the binary with POSIX
// single quotes on Unix and Windows double quotes on Windows (commandLineForOS),
// so the expectation is selected per platform. The "windows quoting" case
// always exercises the Windows path explicitly.
func TestHookCommandGolden(t *testing.T) {
	const binary = "/usr/local/bin/seek"
	cases := []struct {
		name        string
		target      Target
		wantPosix   string
		wantWindows string
	}{
		{
			"claude stop",
			Target{name: "Claude Code", agent: "claude", event: "Stop"},
			"'/usr/local/bin/seek' hooks sync --agent claude",
			`"/usr/local/bin/seek" hooks sync --agent claude`,
		},
		{
			"claude context",
			Target{name: "Claude Code", agent: "claude", event: "UserPromptSubmit", context: true},
			"'/usr/local/bin/seek' hooks context --agent claude",
			`"/usr/local/bin/seek" hooks context --agent claude`,
		},
		{
			"codex stop",
			Target{name: "Codex", agent: "codex", event: "Stop"},
			"'/usr/local/bin/seek' hooks sync --agent codex",
			`"/usr/local/bin/seek" hooks sync --agent codex`,
		},
		{
			"codex interrupt",
			Target{name: "Codex", agent: "codex", event: "Interrupt", background: true, async: true},
			"'/usr/local/bin/seek' hooks sync --agent codex --background",
			`"/usr/local/bin/seek" hooks sync --agent codex --background`,
		},
		{
			"claude stop embed",
			Target{name: "Claude Code", agent: "claude", event: "Stop", embed: true},
			"'/usr/local/bin/seek' hooks sync --agent claude --embed",
			`"/usr/local/bin/seek" hooks sync --agent claude --embed`,
		},
		{
			"windows quoting",
			Target{name: "Codex", agent: "codex", event: "Stop", background: true, async: true},
			`"C:\Program Files\Seek\seek.exe" hooks sync --agent codex --background`,
			`"C:\Program Files\Seek\seek.exe" hooks sync --agent codex --background`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got, want string
			if tc.name == "windows quoting" {
				got = commandLineForOS("windows", `C:\Program Files\Seek\seek.exe`, "hooks", "sync", "--agent", "codex", "--background")
				want = tc.wantWindows
			} else {
				got = hookCommand(tc.target, binary)
				if runtime.GOOS == "windows" {
					want = tc.wantWindows
				} else {
					want = tc.wantPosix
				}
			}
			if got != want {
				t.Errorf("hookCommand = %q, want pinned %q", got, want)
			}
		})
	}
}

func TestRemoveAllSeekEntries_KeepsForeignCommandInMixedEntry(t *testing.T) {
	// A seek command sharing one matcher entry with another tool's command:
	// only seek's command may go; the entry and the foreign command stay.
	path := filepath.Join(t.TempDir(), "settings.json")
	content := `{"hooks": {"Stop": [
		{"hooks": [
			{"type": "command", "command": "other-tool run"},
			{"type": "command", "command": "'seek' hooks sync"}
		]}
	]}}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAllSeekEntries(path); err != nil {
		t.Fatalf("RemoveAllSeekEntries: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "other-tool run") {
		t.Errorf("foreign command in the same entry was removed: %s", data)
	}
	if strings.Contains(string(data), "seek") {
		t.Errorf("seek command still present: %s", data)
	}
	if !strings.Contains(string(data), `"Stop"`) {
		t.Errorf("entry should survive with the foreign command: %s", data)
	}
}

// Two seek targets can share one matcher entry (e.g. a hand-edited settings
// file). Uninstalling one must not delete the other target's command.
func TestUninstallHook_RemovesOnlyRequestedTargetInSharedEntry(t *testing.T) {
	const content = `{"hooks": {"Stop": [
		{"matcher": "", "hooks": [
			{"type": "command", "command": "seek hooks sync --agent claude"},
			{"type": "command", "command": "seek hooks sync --agent codex"}
		]}
	]}}`
	cases := []struct {
		name   string
		remove string
		keep   string
	}{
		{"remove first command", "claude", "codex"},
		{"remove second command", "codex", "claude"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			remove := Target{name: tc.remove, agent: tc.remove, settingsPath: func() string { return path }, event: "Stop"}
			keep := Target{name: tc.keep, agent: tc.keep, settingsPath: func() string { return path }, event: "Stop"}

			if err := uninstallHook(remove, io.Discard); err != nil {
				t.Fatalf("uninstallHook(%s): %v", tc.remove, err)
			}
			settings := readTestSettings(t, path)
			if findTargetSeekHookIndex(settings, remove) >= 0 {
				t.Errorf("%s hook still present after uninstall", tc.remove)
			}
			if findTargetSeekHookIndex(settings, keep) < 0 {
				data, _ := os.ReadFile(path)
				t.Fatalf("%s hook was removed along with %s: %s", tc.keep, tc.remove, data)
			}
		})
	}
}

// TestUninstallHook_MissingTargetLeavesOtherTargetIntact covers the case the
// shared-entry test cannot: only one target is installed, and uninstalling a
// different target must be an honest no-op rather than removing the installed
// hook through the broad legacy matcher.
func TestUninstallHook_MissingTargetLeavesOtherTargetIntact(t *testing.T) {
	const content = `{"hooks": {"Stop": [
		{"matcher": "", "hooks": [
			{"type": "command", "command": "seek hooks sync --agent codex"}
		]}
	]}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	claude := Target{name: "claude", agent: "claude", settingsPath: func() string { return path }, event: "Stop"}
	codex := Target{name: "codex", agent: "codex", settingsPath: func() string { return path }, event: "Stop"}

	var out strings.Builder
	if err := uninstallHook(claude, &out); err != nil {
		t.Fatalf("uninstallHook(claude): %v", err)
	}
	if !strings.Contains(out.String(), "not installed") {
		t.Errorf("output = %q, want a not-installed message", out.String())
	}
	settings := readTestSettings(t, path)
	if findTargetSeekHookIndex(settings, codex) < 0 {
		data, _ := os.ReadFile(path)
		t.Fatalf("codex hook was removed while uninstalling claude: %s", data)
	}
}
