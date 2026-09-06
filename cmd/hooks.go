package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
)

type HooksCmd struct {
	Install   HooksInstallCmd   `cmd:"" help:"Install seek hooks into AI tools"`
	Uninstall HooksUninstallCmd `cmd:"" help:"Remove seek hooks from AI tools"`
}

type HooksInstallCmd struct {
	Claude bool `help:"Install only the Claude Code hook"`
	Codex  bool `help:"Install only the Codex hook"`
}

type HooksUninstallCmd struct {
	Claude bool `help:"Remove only the Claude Code hook"`
	Codex  bool `help:"Remove only the Codex hook"`
}

// hookTarget describes an AI tool whose hook configuration uses the
// Claude-Code-style JSON schema:
//
//	{ "hooks": { "<event>": [ { "matcher": "...", "hooks": [ {"type": "command", "command": "..."} ] } ] } }
type hookTarget struct {
	name         string // display name, e.g. "Claude Code"
	settingsPath func() string
	event        string // hook event, e.g. "Stop"
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

var hookTargets = []hookTarget{
	{name: "Claude Code", settingsPath: claudeSettingsPath, event: "Stop"},
	{name: "Codex", settingsPath: codexHooksPath, event: "Stop"},
}

// selectedTargets filters hookTargets by the --claude/--codex flags.
// When neither flag is given, all targets are selected.
func selectedTargets(claudeOnly, codexOnly bool) []hookTarget {
	if !claudeOnly && !codexOnly {
		return hookTargets
	}
	var out []hookTarget
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

func (c *HooksInstallCmd) Run(cfg *config.AppConfig) error {
	for _, t := range selectedTargets(c.Claude, c.Codex) {
		if err := installHook(t); err != nil {
			return err
		}
	}
	return nil
}

func (c *HooksUninstallCmd) Run(cfg *config.AppConfig) error {
	for _, t := range selectedTargets(c.Claude, c.Codex) {
		if err := uninstallHook(t); err != nil {
			return err
		}
	}
	return nil
}

// --- generic Claude-Code-style JSON hooks ---

func readHookSettings(path string) (map[string]interface{}, error) {
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

func writeHookSettings(path string, settings map[string]interface{}) error {
	os.MkdirAll(filepath.Dir(path), config.DefaultDirPerms)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, config.DefaultFilePerms)
}

func seekBinaryPath() string {
	bin, err := exec.LookPath("seek")
	if err != nil {
		return "seek"
	}
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return bin
	}
	return real
}

func findSeekHookIndex(settings map[string]interface{}, event string) int {
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return -1
	}
	eventHooks, ok := hooks[event].([]interface{})
	if !ok {
		return -1
	}
	for i, entry := range eventHooks {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		hookList, ok := entryMap["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range hookList {
			hMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, _ := hMap["command"].(string)
			if strings.Contains(cmd, "seek sync") {
				return i
			}
		}
	}
	return -1
}

func installHook(t hookTarget) error {
	path := t.settingsPath()
	settings, err := readHookSettings(path)
	if err != nil {
		return err
	}

	if findSeekHookIndex(settings, t.event) >= 0 {
		fmt.Printf("%s hook already installed.\n", t.name)
		return nil
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	eventHooks, ok := hooks[t.event].([]interface{})
	if !ok {
		eventHooks = []interface{}{}
	}

	newHook := map[string]interface{}{
		"matcher": "",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": seekBinaryPath() + " sync",
			},
		},
	}

	eventHooks = append(eventHooks, newHook)
	hooks[t.event] = eventHooks

	if err := writeHookSettings(path, settings); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	fmt.Printf("Installed %s %s hook → seek sync\n", t.name, t.event)
	return nil
}

func uninstallHook(t hookTarget) error {
	path := t.settingsPath()
	settings, err := readHookSettings(path)
	if err != nil {
		return err
	}

	idx := findSeekHookIndex(settings, t.event)
	if idx < 0 {
		fmt.Printf("%s hook not installed.\n", t.name)
		return nil
	}

	hooks := settings["hooks"].(map[string]interface{})
	eventHooks := hooks[t.event].([]interface{})

	eventHooks = append(eventHooks[:idx], eventHooks[idx+1:]...)
	if len(eventHooks) == 0 {
		delete(hooks, t.event)
	} else {
		hooks[t.event] = eventHooks
	}

	if err := writeHookSettings(path, settings); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	fmt.Printf("Removed %s %s hook.\n", t.name, t.event)
	return nil
}
