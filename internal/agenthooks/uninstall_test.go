package agenthooks

import (
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
