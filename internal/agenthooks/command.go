package agenthooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// seekBinaryPath resolves the installed seek binary for hook command
// strings. PATH-first on purpose: settings files must contain a stable,
// user-visible path, and this function's output is a compatibility surface
// (see cmd/service.go seekBinary for the Executable-first variant used by
// service templates — they intentionally differ).
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

// ResolveBinary validates that a binary recorded in an installed hook
// command actually resolves (absolute paths must exist, bare names must be
// on PATH).
func ResolveBinary(binary string) (string, error) {
	if filepath.IsAbs(binary) || strings.ContainsAny(binary, `\/`) {
		if _, err := os.Stat(binary); err != nil {
			return "", err
		}
		return binary, nil
	}
	return exec.LookPath(binary)
}

// hookRuntimeBinary resolves the binary for the subprocess entry points:
// the running executable when invoked by absolute path, else PATH.
func hookRuntimeBinary() string {
	if len(os.Args) > 0 {
		return hookRuntimeBinaryFor(os.Args[0], seekBinaryPath())
	}
	return seekBinaryPath()
}

func hookRuntimeBinaryFor(argv0, fallback string) string {
	if filepath.IsAbs(argv0) || strings.ContainsAny(argv0, `\/`) {
		return argv0
	}
	return fallback
}

// hookCommand returns a JSON-safe hook command for a target.
func hookCommand(t Target, binary string) string {
	command := "sync"
	if t.context {
		command = "context"
	}
	args := []string{"hooks", command}
	if t.agent != "" {
		args = append(args, "--agent", t.agent)
	}
	if t.background && !t.context {
		args = append(args, "--background")
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
	return ShellQuote(binary) + " " + strings.Join(args, " ")
}

// ShellQuote wraps a string in single quotes for safe POSIX shell embedding.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func findSeekHookIndex(settings map[string]interface{}, event string) int {
	return findHookIndex(settings, event, isSeekHookCommand)
}

// findTargetSeekHookIndex only recognizes the exact current command shape and
// required runner options for target. Legacy commands remain discoverable by
// findSeekHookIndex so install and uninstall can upgrade or remove them, but
// are not reported as healthy.
func findTargetSeekHookIndex(settings map[string]interface{}, target Target) int {
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return -1
	}
	eventHooks, ok := hooks[target.event].([]interface{})
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
		for _, value := range hookList {
			hook, ok := value.(map[string]interface{})
			if !ok {
				continue
			}
			command, _ := hook["command"].(string)
			if isTargetSeekHookCommand(command, target) && targetHookOptionsMatch(hook, target) {
				return i
			}
		}
	}
	return -1
}

func targetHookOptionsMatch(hook map[string]interface{}, target Target) bool {
	if !target.async {
		return true
	}
	async, ok := hook["async"].(bool)
	return ok && async
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
	// the seek executable: quoted POSIX/Windows paths, bare "seek", and quoted
	// bare 'seek' — the quoted bare form is what install writes when
	// exec.LookPath("seek") cannot resolve the binary (e.g. CI), so install,
	// upgrade and uninstall must all still find their own hook entry.
	// Quoted forms must end in a path separator + "seek" (or be exactly the
	// bare binary name) so lookalikes like 'myseek' never match.
	directSeekHookCommandPattern = regexp.MustCompile(`(?i)^(?:'(?:(?:[^'/\\]*[\\/]+)*seek(?:\.exe)?)'|"(?:(?:[^"\\]*[\\/]+)*seek(?:\.exe)?)"|(?:[^\s]+[\\/])?seek(?:\.exe)?)\s+(?:hooks\s+)?(?:sync|context)(?:\s+--(?:agent\s+\S+|embed|background))*\s*$`)
)

func isSeekHookCommand(command string) bool {
	return directSeekHookCommandPattern.MatchString(command) || isLegacyCodexWrapper(command)
}

func isTargetSeekHookCommand(command string, target Target) bool {
	if !isSeekHookCommand(command) {
		return false
	}
	commandName := "sync"
	if target.context {
		commandName = "context"
	}
	pattern := `(?i)^(?:'(?:(?:[^'/\\]*[\\/]+)*seek(?:\.exe)?)'|"(?:(?:[^"\\]*[\\/]+)*seek(?:\.exe)?)"|(?:[^\s]+[\\/])?seek(?:\.exe)?)\s+hooks\s+` + commandName + `\s+--agent\s+` + regexp.QuoteMeta(target.agent)
	if !target.context {
		if target.background {
			pattern += `\s+--background`
		}
		pattern += `(?:\s+--embed)?`
	}
	return regexp.MustCompile(pattern + `\s*$`).MatchString(command)
}

var hookExecutablePattern = regexp.MustCompile(`^(?:'([^']+)'|"([^"]+)"|(\S+))\s+hooks\s+`)

func hookCommandBinary(settings map[string]interface{}, target Target) (string, bool) {
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
