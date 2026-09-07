package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/google/renameio"
	"github.com/ozgurulukir/seek/internal/config"
)

type HooksCmd struct {
	Install   HooksInstallCmd   `cmd:"" help:"Install seek hooks into AI tools"`
	Uninstall HooksUninstallCmd `cmd:"" help:"Remove seek hooks from AI tools"`
	Status    HooksStatusCmd    `cmd:"" help:"Show installed hook health and recent activity"`
	Doctor    HooksDoctorCmd    `cmd:"" help:"Diagnose hook installation and binary resolution"`
	Context   HooksContextCmd   `cmd:"" hidden:""`
	Sync      HooksSyncCmd      `cmd:"" hidden:""`
}

type HooksInstallCmd struct {
	Claude bool `help:"Install only the Claude Code hook"`
	Codex  bool `help:"Install only the Codex hook"`
	Embed  bool `help:"Embed new conversation chunks after sync"`
}

type HooksUninstallCmd struct {
	Claude bool `help:"Remove only the Claude Code hook"`
	Codex  bool `help:"Remove only the Codex hook"`
}

// HooksSyncCmd is the machine-readable hook entry point. It intentionally
// suppresses sync progress and errors: Codex requires valid JSON on stdout.
type HooksSyncCmd struct {
	Agent    string        `hidden:"" help:"Collection type to sync"`
	Embed    bool          `hidden:"" help:"Embed newly synced chunks"`
	Debounce time.Duration `hidden:"" default:"15s" help:"Minimum time between hook syncs"`
}

type HooksStatusCmd struct{}

type HooksDoctorCmd struct{}

type HooksContextCmd struct {
	Agent string `hidden:""`
}

const (
	hookContextTimeout   = 3 * time.Second
	hookContextWaitDelay = 500 * time.Millisecond
	hookContextMaxBytes  = 4000
	hookLockWaitTimeout  = 5 * time.Minute
	hookSyncTimeout      = 3 * time.Minute
	hookEmbedTimeout     = 90 * time.Second
	hookLockEnv          = "SEEK_INTERNAL_WRITER_LOCK"
)

// hookTarget describes an AI tool whose hook configuration uses the
// Claude-Code-style JSON schema:
//
//	{ "hooks": { "<event>": [ { "matcher": "...", "hooks": [ {"type": "command", "command": "..."} ] } ] } }
type hookTarget struct {
	name          string // display name, e.g. "Claude Code"
	agent         string // collection type, e.g. "claude"
	settingsPath  func() string
	event         string // hook event, e.g. "Stop"
	context       bool   // Context hooks search seek and return additional agent context.
	embed         bool   // Sync hooks optionally refresh semantic search.
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
	{name: "Claude Code", agent: "claude", settingsPath: claudeSettingsPath, event: "Stop", timeout: 600, statusMessage: "Syncing seek index..."},
	{name: "Claude Code", agent: "claude", settingsPath: claudeSettingsPath, event: "UserPromptSubmit", context: true, timeout: 5},
	{name: "Codex", agent: "codex", settingsPath: codexHooksPath, event: "Stop", timeout: 600},
	{name: "Codex", agent: "codex", settingsPath: codexHooksPath, event: "UserPromptSubmit", context: true, timeout: 5},
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
		t.embed = c.Embed
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

func (c *HooksStatusCmd) Run(cfg *config.AppConfig) error {
	var statusErr error
	for _, target := range hookTargets {
		settings, err := readHookSettings(target.settingsPath())
		if err != nil {
			fmt.Printf("%s %s: unavailable (%v)\n", target.name, target.event, err)
			statusErr = errors.Join(statusErr, err)
			continue
		}
		installed := findTargetSeekHookIndex(settings, target) >= 0
		if target.context {
			fmt.Printf("%s %s: installed=%t\n", target.name, target.event, installed)
			continue
		}
		state, hasState := readHookState(hookStatePath(cfg, target.agent))
		status := "never run"
		if hasState {
			if state.SkippedReason != "" && state.LastAttemptAt.After(state.CompletedAt) {
				status = "skipped at " + state.LastAttemptAt.Local().Format(time.RFC3339) + " (" + state.SkippedReason + ")"
			} else {
				status = state.CompletedAt.Local().Format(time.RFC3339)
			}
			if state.Error != "" && state.SkippedReason == "" {
				status += " (last error: " + state.Error + ")"
			}
		}
		fmt.Printf("%s %s: installed=%t, last sync=%s\n", target.name, target.event, installed, status)
	}
	return statusErr
}

func (c *HooksDoctorCmd) Run(cfg *config.AppConfig) error {
	var doctorErr error
	for _, target := range hookTargets {
		settings, err := readHookSettings(target.settingsPath())
		if err != nil {
			fmt.Printf("%s %s: unavailable (%v)\n", target.name, target.event, err)
			doctorErr = errors.Join(doctorErr, err)
			continue
		}
		if binary, ok := hookCommandBinary(settings, target); ok {
			resolved, err := resolveHookBinary(binary)
			if err != nil {
				fmt.Printf("%s %s binary: %s (unavailable: %v)\n", target.name, target.event, binary, err)
			} else {
				fmt.Printf("%s %s binary: %s\n", target.name, target.event, resolved)
			}
		}
	}
	return errors.Join(doctorErr, (&HooksStatusCmd{}).Run(cfg))
}

func (c *HooksContextCmd) Run(cfg *config.AppConfig) error {
	input, readErr := io.ReadAll(io.LimitReader(os.Stdin, 64*1024))
	if readErr != nil {
		fmt.Fprintf(os.Stderr, "WARN: read hook input: %v\n", readErr)
	}
	prompt := hookPrompt(input)
	contextText := ""
	if prompt != "" {
		ctx, cancel := context.WithTimeout(context.Background(), hookContextTimeout)
		defer cancel()
		command := exec.CommandContext(ctx, hookRuntimeBinary(), hookSearchArgs(c.Agent, prompt)...)
		command.WaitDelay = hookContextWaitDelay
		var output strings.Builder
		command.Stdout = &limitedWriter{writer: &output, remaining: hookContextMaxBytes}
		if err := command.Run(); err == nil {
			contextText = output.String()
		} else {
			fmt.Fprintf(os.Stderr, "WARN: hook context search: %v\n", err)
		}
	}
	return json.NewEncoder(os.Stdout).Encode(hookContextResponse(contextText))
}

func hookContextResponse(contextText string) map[string]interface{} {
	return map[string]interface{}{
		"additionalContext": contextText,
		"hookSpecificOutput": map[string]string{
			"hookEventName":     "UserPromptSubmit",
			"additionalContext": contextText,
		},
	}
}

func hookSearchArgs(agent, prompt string) []string {
	args := []string{"search", prompt, "--lex", "-l", "3"}
	if agent != "" {
		args = append(args, "--doc-type", agent)
	}
	return args
}

// limitedWriter retains at most remaining bytes but reports the whole write as
// consumed, allowing a child process to complete without buffering its output.
type limitedWriter struct {
	writer    io.Writer
	remaining int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		return len(p), nil
	}
	kept := p
	if len(kept) > w.remaining {
		kept = kept[:w.remaining]
	}
	n, err := w.writer.Write(kept)
	w.remaining -= n
	if err != nil {
		return n, err
	}
	return len(p), nil
}

func hookPrompt(input []byte) string {
	var event map[string]interface{}
	if json.Unmarshal(input, &event) != nil {
		return ""
	}
	for _, key := range []string{"prompt", "user_prompt", "text"} {
		if value, ok := event[key].(string); ok {
			return value
		}
	}
	return ""
}

func (c *HooksSyncCmd) Run(cfg *config.AppConfig) error {
	if !validHookAgent(c.Agent) {
		fmt.Fprintf(os.Stderr, "WARN: invalid seek hook agent %q\n", c.Agent)
		_, err := io.WriteString(os.Stdout, "{}\n")
		return err
	}
	statePath := hookStatePath(cfg, c.Agent)
	lockCtx, cancelLock := context.WithTimeout(context.Background(), hookLockWaitTimeout)
	defer cancelLock()
	lock, err := acquireHookLock(lockCtx, hookSyncLockPath(cfg))
	if err != nil {
		latest, _ := readHookState(statePath)
		recordHookSkip(statePath, latest, "writer lock unavailable: "+err.Error())
		_, writeErr := io.WriteString(os.Stdout, "{}\n")
		return writeErr
	}
	defer lock.Close()
	state, hasState := readHookState(statePath)
	if hasState && time.Since(state.CompletedAt) < c.Debounce {
		recordHookSkip(statePath, state, "debounced")
		_, err := io.WriteString(os.Stdout, "{}\n")
		return err
	}
	args := []string{"sync", "--no-lock"}
	if c.Agent != "" {
		args = append(args, "--type", c.Agent)
	}
	syncCtx, cancelSync := context.WithTimeout(context.Background(), hookSyncTimeout)
	defer cancelSync()
	var childErr error
	syncErr := runHooksSync(func() error {
		command := exec.CommandContext(syncCtx, hookRuntimeBinary(), args...)
		command.Env = withHookLockEnv(os.Environ())
		command.WaitDelay = hookContextWaitDelay
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		childErr = command.Run()
		return childErr
	}, os.Stdout)
	if childErr == nil && c.Embed {
		embedCtx, cancelEmbed := context.WithTimeout(context.Background(), hookEmbedTimeout)
		defer cancelEmbed()
		embedArgs := []string{"embed", "--no-lock"}
		if c.Agent != "" {
			embedArgs = append(embedArgs, "--type", c.Agent)
		}
		embedArgs = append(embedArgs, "--realtime")
		embed := exec.CommandContext(embedCtx, hookRuntimeBinary(), embedArgs...)
		embed.Env = withHookLockEnv(os.Environ())
		embed.WaitDelay = hookContextWaitDelay
		embed.Stdout = io.Discard
		embed.Stderr = io.Discard
		childErr = embed.Run()
	}
	if err := writeHookState(statePath, hookState{Agent: c.Agent, CompletedAt: time.Now(), LastAttemptAt: time.Now(), Error: errorString(childErr)}); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: record seek hook state: %v\n", err)
	}
	return syncErr
}

type hookState struct {
	Agent         string    `json:"agent"`
	CompletedAt   time.Time `json:"completed_at"`
	LastAttemptAt time.Time `json:"last_attempt_at"`
	SkippedReason string    `json:"skipped_reason,omitempty"`
	Error         string    `json:"error,omitempty"`
}

func hookStatePath(cfg *config.AppConfig, agent string) string {
	return filepath.Join(cfg.CacheDir, "hooks", agent+".json")
}

func hookSyncLockPath(cfg *config.AppConfig) string {
	return filepath.Join(cfg.CacheDir, "hooks", "sync.lock")
}

func validHookAgent(agent string) bool {
	return agent == "" || agent == "claude" || agent == "codex"
}

func hookRuntimeBinary() string {
	if len(os.Args) > 0 {
		return hookRuntimeBinaryFor(os.Args[0], seekBinaryPath())
	}
	return seekBinaryPath()
}

func hookRuntimeBinaryFor(argv0, fallback string) string {
	if filepath.IsAbs(argv0) || strings.ContainsAny(argv0, `\\/`) {
		return argv0
	}
	return fallback
}

func withHookLockEnv(env []string) []string {
	filtered := env[:0]
	for _, value := range env {
		if !strings.HasPrefix(value, hookLockEnv+"=") {
			filtered = append(filtered, value)
		}
	}
	return append(filtered, hookLockEnv+"=1")
}

func readHookState(path string) (hookState, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return hookState{}, false
	}
	var state hookState
	return state, json.Unmarshal(data, &state) == nil
}

func recordHookSkip(path string, state hookState, reason string) {
	state.LastAttemptAt = time.Now()
	state.SkippedReason = reason
	if err := writeHookState(path, state); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: record skipped seek hook: %v\n", err)
	}
}

func writeHookState(path string, state hookState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return renameio.WriteFile(path, data, config.DefaultFilePerms)
}

func acquireHookLock(ctx context.Context, path string) (*hookLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), config.DefaultDirPerms); err != nil {
		return nil, err
	}
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, config.DefaultFilePerms)
		if err != nil {
			return nil, err
		}
		lock, err := lockHookFile(file)
		if err == nil {
			return lock, nil
		}
		_ = file.Close()
		if !isHookLockBusy(err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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

func resolveHookBinary(binary string) (string, error) {
	if filepath.IsAbs(binary) || strings.ContainsAny(binary, `\\/`) {
		if _, err := os.Stat(binary); err != nil {
			return "", err
		}
		return binary, nil
	}
	return exec.LookPath(binary)
}

// hookCommand returns a JSON-safe hook command for a target.
func hookCommand(t hookTarget, binary string) string {
	command := "sync"
	if t.context {
		command = "context"
	}
	args := []string{"hooks", command}
	if t.agent != "" {
		args = append(args, "--agent", t.agent)
	}
	if t.embed && !t.context {
		args = append(args, "--embed")
	}
	return commandLine(binary, args...)
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
	return findHookIndex(settings, event, isSeekHookCommand)
}

// findTargetSeekHookIndex only recognizes the exact current command shape for
// target. Legacy commands remain discoverable by findSeekHookIndex so install
// and uninstall can upgrade or remove them, but are not reported as healthy.
func findTargetSeekHookIndex(settings map[string]interface{}, target hookTarget) int {
	return findHookIndex(settings, target.event, func(command string) bool {
		return isTargetSeekHookCommand(command, target)
	})
}

func findHookIndex(settings map[string]interface{}, event string, matches func(string) bool) int {
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
			if matches(cmd) {
				return i
			}
		}
	}
	return -1
}

var (
	// directSeekHookCommandPattern accepts only a complete direct invocation of
	// the seek executable, including quoted POSIX and Windows paths.
	directSeekHookCommandPattern = regexp.MustCompile(`(?i)^(?:'[^']*[\\/]seek(?:\.exe)?'|"[^"]*[\\/]seek(?:\.exe)?"|(?:[^\s]+[\\/])?seek(?:\.exe)?)\s+(?:hooks\s+)?(?:sync|context)(?:\s+--(?:agent\s+\S+|embed))*\s*$`)
)

func isSeekHookCommand(command string) bool {
	return directSeekHookCommandPattern.MatchString(command) || isLegacyCodexWrapper(command)
}

func isTargetSeekHookCommand(command string, target hookTarget) bool {
	if !isSeekHookCommand(command) {
		return false
	}
	commandName := "sync"
	if target.context {
		commandName = "context"
	}
	pattern := `(?i)^(?:'[^']*[\\/]seek(?:\.exe)?'|"[^"]*[\\/]seek(?:\.exe)?"|(?:[^\s]+[\\/])?seek(?:\.exe)?)\s+hooks\s+` + commandName + `\s+--agent\s+` + regexp.QuoteMeta(target.agent)
	if !target.context {
		pattern += `(?:\s+--embed)?`
	}
	return regexp.MustCompile(pattern + `\s*$`).MatchString(command)
}

var hookExecutablePattern = regexp.MustCompile(`^(?:'([^']+)'|"([^"]+)"|(\S+))\s+hooks\s+`)

func hookCommandBinary(settings map[string]interface{}, target hookTarget) (string, bool) {
	idx := findTargetSeekHookIndex(settings, target)
	if idx < 0 {
		return "", false
	}
	hooks := settings["hooks"].(map[string]interface{})
	entry := hooks[target.event].([]interface{})[idx].(map[string]interface{})
	for _, h := range entry["hooks"].([]interface{}) {
		hook, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		command, _ := hook["command"].(string)
		if !isTargetSeekHookCommand(command, target) {
			continue
		}
		matches := hookExecutablePattern.FindStringSubmatch(command)
		for _, binary := range matches[1:] {
			if binary != "" {
				return binary, true
			}
		}
	}
	return "", false
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
		if !t.context && seekHookUsesEmbed(settings, t.event, idx) {
			t.embed = true
			command = hookCommand(t, seekBinaryPath())
		}
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
