package agenthooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// The context hook budget: a short search runs inside the hook runner's
// timeout, and its output is capped so the agent prompt stays small.
const (
	hookContextTimeout   = 3 * time.Second
	hookContextWaitDelay = 500 * time.Millisecond
	hookContextMaxBytes  = 4000
)

// RunContext is the `seek hooks context` entry point the installed
// UserPromptSubmit hooks execute: it reads the hook event JSON from stdin,
// runs a keyword search for the prompt, and emits the Claude-Code-style
// hookSpecificOutput envelope on stdout.
func RunContext(agent string, stdin io.Reader, stdout, stderr io.Writer) error {
	input, readErr := io.ReadAll(io.LimitReader(stdin, 64*1024))
	if readErr != nil {
		fmt.Fprintf(stderr, "WARN: read hook input: %v\n", readErr)
	}
	prompt := hookPrompt(input)
	contextText := ""
	if prompt != "" {
		ctx, cancel := context.WithTimeout(context.Background(), hookContextTimeout)
		defer cancel()
		command := exec.CommandContext(ctx, hookRuntimeBinary(), hookSearchArgs(agent, prompt)...)
		command.WaitDelay = hookContextWaitDelay
		var output strings.Builder
		command.Stdout = &limitedWriter{writer: &output, remaining: hookContextMaxBytes}
		if err := command.Run(); err == nil {
			contextText = output.String()
		} else {
			fmt.Fprintf(stderr, "WARN: hook context search: %v\n", err)
		}
	}
	return json.NewEncoder(stdout).Encode(hookContextResponse(contextText))
}

func hookContextResponse(contextText string) map[string]interface{} {
	return map[string]interface{}{
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
