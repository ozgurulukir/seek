package agenthooks

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Install writes (or upgrades in place) the target's seek hook. embed
// records the user's --embed choice; an existing hook's embed flag wins so a
// repair never silently downgrades it. Progress messages go to out.
func Install(t Target, embed bool, out io.Writer) error {
	if embed {
		t.embed = true
	}
	return installHook(t, out)
}

// Uninstall removes the target's seek hook entry, leaving every other tool's
// configuration untouched. Progress messages go to out.
func Uninstall(t Target, out io.Writer) error {
	return uninstallHook(t, out)
}

// RemoveAllSeekEntries strips every seek hook entry from one agent settings
// file without touching anything else. It iterates ALL events (not just the
// ones whose target settingsPath equals path — callers may pass an arbitrary
// settings file), applying the same seek-command matcher the per-target
// uninstall uses. A missing file is a no-op.
func RemoveAllSeekEntries(path string) error {
	settings, err := ReadSettings(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	changed := false
	for _, event := range hookEvents() {
		for {
			idx := findSeekHookIndex(settings, event)
			if idx < 0 {
				break
			}
			if err := removeHookAtIndex(settings, event, idx); err != nil {
				return err
			}
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return WriteSettings(path, settings)
}

func installHook(t Target, out io.Writer) error {
	path := t.settingsPath()
	settings, err := ReadSettings(path)
	if err != nil {
		return err
	}

	command := hookCommand(t, seekBinaryPath())
	if idx := findSeekHookIndex(settings, t.event); idx >= 0 {
		if !t.context && seekHookUsesEmbed(settings, t.event, idx) {
			t.embed = true
			command = hookCommand(t, seekBinaryPath())
		}
		if replaceSeekHookCommand(settings, t.event, idx, command, t) {
			if err := WriteSettings(path, settings); err != nil {
				return fmt.Errorf("write %s: %w", filepath.Base(path), err)
			}
			fmt.Fprintf(out, "Updated %s %s hook.\n", t.name, t.event)
			return nil
		}
		fmt.Fprintf(out, "%s hook already installed.\n", t.name)
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
	if t.async {
		commandHook["async"] = true
	}

	newHook := map[string]interface{}{
		"matcher": "",
		"hooks": []interface{}{
			commandHook,
		},
	}

	eventHooks = append(eventHooks, newHook)
	hooks[t.event] = eventHooks

	if err := WriteSettings(path, settings); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	fmt.Fprintf(out, "Installed %s %s hook → %s\n", t.name, t.event, command)
	return nil
}

func seekHookUsesEmbed(settings map[string]interface{}, event string, idx int) bool {
	hooks := settings["hooks"].(map[string]interface{})
	entry := hooks[event].([]interface{})[idx].(map[string]interface{})
	for _, h := range entry["hooks"].([]interface{}) {
		hook, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		command, _ := hook["command"].(string)
		if isSeekHookCommand(command) && strings.Contains(command, "--embed") {
			return true
		}
	}
	return false
}

// replaceSeekHookCommand upgrades a seek hook in place without disturbing
// other commands in the same event entry.
func replaceSeekHookCommand(settings map[string]interface{}, event string, idx int, command string, target Target) bool {
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
			if target.async {
				if async, ok := hook["async"].(bool); !ok || !async {
					hook["async"] = true
					changed = true
				}
			}
			return changed
		}
	}
	return false
}

func uninstallHook(t Target, out io.Writer) error {
	path := t.settingsPath()
	settings, err := ReadSettings(path)
	if err != nil {
		return err
	}

	idx := findSeekHookIndex(settings, t.event)
	if idx < 0 {
		fmt.Fprintf(out, "%s hook not installed.\n", t.name)
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

	if err := WriteSettings(path, settings); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	fmt.Fprintf(out, "Removed %s %s hook.\n", t.name, t.event)
	return nil
}

// removeHookAtIndex deletes the entry at idx from the event's hook list and
// cleans up the event when it becomes empty (same semantics as uninstallHook).
func removeHookAtIndex(settings map[string]interface{}, event string, idx int) error {
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("no hooks map")
	}
	eventHooks, ok := hooks[event].([]interface{})
	if !ok || idx >= len(eventHooks) {
		return fmt.Errorf("hook entry vanished")
	}
	eventHooks = append(eventHooks[:idx], eventHooks[idx+1:]...)
	if len(eventHooks) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = eventHooks
	}
	return nil
}
