package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/google/renameio"
	"github.com/ozgurulukir/seek/internal/config"
)

type HooksCmd struct {
	Install   HooksInstallCmd   `cmd:"" help:"Install seek hooks into AI tools"`
	Uninstall HooksUninstallCmd `cmd:"" help:"Remove seek hooks from AI tools"`
	Sync      HooksSyncCmd      `cmd:"" hidden:""`
}

type HooksInstallCmd struct {
	Claude bool `help:"Install only the Claude Code hook"`
	Codex  bool `help:"Install only the Codex hook"`
}

type HooksUninstallCmd struct {
	Claude bool `help:"Remove only the Claude Code hook"`
	Codex  bool `help:"Remove only the Codex hook"`
}

// HooksSyncCmd is the machine-readable hook entry point. It intentionally
// suppresses sync progress and errors: Codex requires valid JSON on stdout.
type HooksSyncCmd struct{}

// hookTarget describes an AI tool whose hook configuration uses the
// Claude-Code-style JSON schema:
//
//	{ "hooks": { "<event>": [ { "matcher": "...", "hooks": [ {"type": "command", "command": "..."} ] } ] } }
type hookTarget struct {
	name          string // display name, e.g. "Claude Code"
	settingsPath  func() string
	event         string // hook event, e.g. "Stop"
	codexOutput   bool   // Codex requires command hooks to write JSON to stdout.
	timeout       int    // Optional command timeout in seconds.
	statusMessage string // Optional command status shown by the hook runner.
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
	{name: "Claude Code", settingsPath: claudeSettingsPath, event: "Stop", timeout: 60, statusMessage: "Syncing seek index..."},
	{name: "Codex", settingsPath: codexHooksPath, event: "Stop", codexOutput: true},
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

func (c *HooksSyncCmd) Run(cfg *config.AppConfig) error {
	return runHooksSync(func() error {
		command := exec.Command(seekBinaryPath(), "sync")
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		return command.Run()
	}, os.Stdout)
}

type syncRunner func() error

func runHooksSync(sync syncRunner, output io.Writer) error {
	_ = sync()
	_, err := io.WriteString(output, "{}\n")
	return err
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
	if err := os.MkdirAll(filepath.Dir(path), config.DefaultDirPerms); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return renameio.WriteFile(path, data, config.DefaultFilePerms)
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

// hookCommand returns the hook command for a target. Codex uses the internal
// JSON-only hook entry point, avoiding platform-specific shell wrappers.
func hookCommand(t hookTarget, binary string) string {
	if t.codexOutput {
		return commandLine(binary, "hooks", "sync")
	}
	return commandLine(binary, "sync")
}

func commandLine(binary string, args ...string) string {
	return commandLineForOS(runtime.GOOS, binary, args...)
}

func commandLineForOS(goos, binary string, args ...string) string {
	if goos == "windows" {
		return `"` + strings.ReplaceAll(binary, `"`, `\"`) + `" ` + strings.Join(args, " ")
	}
	return shellQuote(binary) + " " + strings.Join(args, " ")
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
			if isSeekHookCommand(cmd) {
				return i
			}
		}
	}
	return -1
}

var (
	// directSeekHookCommandPattern accepts only a complete direct invocation of
	// the seek executable, including quoted POSIX and Windows paths.
	directSeekHookCommandPattern = regexp.MustCompile(`(?i)^(?:'[^']*[\\/]seek(?:\.exe)?'|"[^"]*[\\/]seek(?:\.exe)?"|(?:[^\s]+[\\/])?seek(?:\.exe)?)\s+(?:hooks\s+)?sync\s*$`)
)

func isSeekHookCommand(command string) bool {
	return directSeekHookCommandPattern.MatchString(command) || isLegacyCodexWrapper(command)
}

func isLegacyCodexWrapper(command string) bool {
	const prefix = "sh -c "
	if !strings.HasPrefix(command, prefix) {
		return false
	}
	body := strings.TrimPrefix(command, prefix)
	if len(body) < 2 || (body[0] != '\'' && body[0] != '"') || body[len(body)-1] != body[0] {
		return false
	}
	body = body[1 : len(body)-1]
	syncPart, outputPart, ok := strings.Cut(body, "; printf ")
	if !ok {
		return false
	}
	const syncSuffix = " sync >/dev/null 2>&1"
	if !strings.HasSuffix(syncPart, syncSuffix) || !isSeekExecutable(strings.TrimSuffix(syncPart, syncSuffix)) {
		return false
	}
	outputPart = strings.Trim(outputPart, "'\"")
	if outputPart == "{}" {
		return true
	}
	return strings.HasPrefix(outputPart, "{}") && strings.Trim(outputPart[2:], `\`) == "n"
}

func isSeekExecutable(value string) bool {
	value = strings.Trim(value, "'\"")
	value = strings.ReplaceAll(value, `\`, "/")
	if slash := strings.LastIndex(value, "/"); slash >= 0 {
		value = value[slash+1:]
	}
	return strings.EqualFold(value, "seek") || strings.EqualFold(value, "seek.exe")
}

func installHook(t hookTarget) error {
	path := t.settingsPath()
	settings, err := readHookSettings(path)
	if err != nil {
		return err
	}

	command := hookCommand(t, seekBinaryPath())
	if idx := findSeekHookIndex(settings, t.event); idx >= 0 {
		if replaceSeekHookCommand(settings, t.event, idx, command, t) {
			if err := writeHookSettings(path, settings); err != nil {
				return fmt.Errorf("write %s: %w", filepath.Base(path), err)
			}
			fmt.Printf("Updated %s %s hook.\n", t.name, t.event)
			return nil
		}
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

	commandHook := map[string]interface{}{
		"type":    "command",
		"command": command,
	}
	if t.timeout > 0 {
		commandHook["timeout"] = t.timeout
	}
	if t.statusMessage != "" {
		commandHook["statusMessage"] = t.statusMessage
	}

	newHook := map[string]interface{}{
		"matcher": "",
		"hooks": []interface{}{
			commandHook,
		},
	}

	eventHooks = append(eventHooks, newHook)
	hooks[t.event] = eventHooks

	if err := writeHookSettings(path, settings); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	fmt.Printf("Installed %s %s hook → %s\n", t.name, t.event, command)
	return nil
}

// replaceSeekHookCommand upgrades a seek hook in place without disturbing
// other commands in the same event entry.
func replaceSeekHookCommand(settings map[string]interface{}, event string, idx int, command string, target hookTarget) bool {
	hooks := settings["hooks"].(map[string]interface{})
	eventHooks := hooks[event].([]interface{})
	entry, ok := eventHooks[idx].(map[string]interface{})
	if !ok {
		return false
	}
	hookList, ok := entry["hooks"].([]interface{})
	if !ok {
		return false
	}
	for _, h := range hookList {
		hook, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if current, _ := hook["command"].(string); isSeekHookCommand(current) {
			changed := false
			if current == command {
				// Keep the existing command when it already has the desired form.
			} else {
				hook["command"] = command
				changed = true
			}
			if target.timeout > 0 {
				currentTimeout, ok := hook["timeout"].(float64)
				if !ok || int(currentTimeout) != target.timeout {
					hook["timeout"] = target.timeout
					changed = true
				}
			}
			if target.statusMessage != "" && hook["statusMessage"] != target.statusMessage {
				hook["statusMessage"] = target.statusMessage
				changed = true
			}
			return changed
		}
	}
	return false
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
	entry := eventHooks[idx].(map[string]interface{})
	hookList := entry["hooks"].([]interface{})
	for hookIdx, hook := range hookList {
		hookMap, ok := hook.(map[string]interface{})
		if !ok {
			continue
		}
		command, _ := hookMap["command"].(string)
		if isSeekHookCommand(command) {
			hookList = append(hookList[:hookIdx], hookList[hookIdx+1:]...)
			break
		}
	}

	if len(hookList) == 0 {
		eventHooks = append(eventHooks[:idx], eventHooks[idx+1:]...)
	} else {
		entry["hooks"] = hookList
	}
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
