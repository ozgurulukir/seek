package agenthooks

import (
	"os"
	"path/filepath"
)

// Target describes an AI tool whose hook configuration uses the
// Claude-Code-style JSON schema:
//
//	{ "hooks": { "<event>": [ { "matcher": "...", "hooks": [ {"type": "command", "command": "..."} ] } ] } }
type Target struct {
	name          string // display name, e.g. "Claude Code"
	agent         string // collection type, e.g. "claude"
	settingsPath  func() string
	event         string // hook event, e.g. "Stop"
	context       bool   // Context hooks search seek and return additional agent context.
	embed         bool   // Sync hooks optionally refresh semantic search.
	background    bool   // Launch a detached worker before the hook runner timeout.
	async         bool   // Ask the hook runner not to wait for the command.
	timeout       int    // Optional command timeout in seconds.
	statusMessage string // Optional command status shown by the hook runner.
}

// Name returns the tool's display name (e.g. "Claude Code").
func (t Target) Name() string { return t.name }

// Agent returns the collection type the hook syncs (e.g. "claude").
func (t Target) Agent() string { return t.agent }

// Event returns the hook event name (e.g. "Stop").
func (t Target) Event() string { return t.event }

// IsContext reports whether the hook is a UserPromptSubmit context hook
// rather than a sync hook.
func (t Target) IsContext() bool { return t.context }

// SettingsPath returns the settings file the target installs into. The
// injected function is the test seam.
func (t Target) SettingsPath() string { return t.settingsPath() }

// Installed reports whether the settings file contains a hook entry matching
// this target's exact current command shape and required runner options.
// Legacy commands remain discoverable via FindSeekHookIndex (for upgrades)
// but are not reported as healthy.
func (t Target) Installed(settings map[string]interface{}) bool {
	return findTargetSeekHookIndex(settings, t) >= 0
}

// CommandBinary returns the seek binary path recorded in the installed hook
// command for this target, if any.
func (t Target) CommandBinary(settings map[string]interface{}) (string, bool) {
	return hookCommandBinary(settings, t)
}

// claudeSettingsPath returns the path to Claude Code settings.json.
func claudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// codexHooksPath returns the path to the Codex hooks.json file.
func codexHooksPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "hooks.json")
}

var hookTargets = []Target{
	{name: "Claude Code", agent: "claude", settingsPath: claudeSettingsPath, event: "Stop", timeout: 600, statusMessage: "Syncing seek index..."},
	{name: "Claude Code", agent: "claude", settingsPath: claudeSettingsPath, event: "UserPromptSubmit", context: true, timeout: 5},
	{name: "Codex", agent: "codex", settingsPath: codexHooksPath, event: "Stop", timeout: 600},
	{name: "Codex", agent: "codex", settingsPath: codexHooksPath, event: "Interrupt", background: true, async: true, timeout: 3},
	{name: "Codex", agent: "codex", settingsPath: codexHooksPath, event: "UserPromptSubmit", context: true, timeout: 5},
}

// Targets returns a copy of the hook target registry in canonical order.
// Adding a new agent = one registry row; no other code path changes.
func Targets() []Target {
	out := make([]Target, len(hookTargets))
	copy(out, hookTargets)
	return out
}

// Selected filters the registry by the --claude/--codex flags. When neither
// flag is given, all targets are selected.
func Selected(claudeOnly, codexOnly bool) []Target {
	if !claudeOnly && !codexOnly {
		return Targets()
	}
	var out []Target
	for _, t := range hookTargets {
		if claudeOnly && t.name == "Claude Code" {
			out = append(out, t)
		}
		if codexOnly && t.name == "Codex" {
			out = append(out, t)
		}
	}
	return out
}

// hookEvents returns the distinct hook event names from the target table.
func hookEvents() []string {
	seen := map[string]bool{}
	var events []string
	for _, t := range hookTargets {
		if !seen[t.event] {
			seen[t.event] = true
			events = append(events, t.event)
		}
	}
	return events
}
