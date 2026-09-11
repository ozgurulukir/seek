package agenthooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/renameio"
	"github.com/ozgurulukir/seek/internal/config"
)

// State records the outcome of the last hook-triggered sync for one agent.
// The JSON tags are a persisted contract (seek hooks status reads them).
type State struct {
	Agent         string    `json:"agent"`
	CompletedAt   time.Time `json:"completed_at"`
	LastAttemptAt time.Time `json:"last_attempt_at"`
	SkippedReason string    `json:"skipped_reason,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// StatePath returns the per-agent hook state file under the cache dir.
func StatePath(cfg *config.AppConfig, agent string) string {
	return filepath.Join(cfg.CacheDir, "hooks", agent+".json")
}

// ReadState loads the hook state file; ok is false when absent or invalid.
func ReadState(path string) (State, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, false
	}
	var state State
	return state, json.Unmarshal(data, &state) == nil
}

// recordHookSkip notes a skipped sync (debounced, lock unavailable) without
// disturbing the last successful completion time.
func recordHookSkip(path string, state State, reason string) {
	state.LastAttemptAt = time.Now()
	state.SkippedReason = reason
	if err := WriteState(path, state); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: record skipped seek hook: %v\n", err)
	}
}

// WriteState persists the hook state atomically with private permissions.
func WriteState(path string, state State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	// State carries agent/session context — private to the user.
	return renameio.WriteFile(path, data, config.DefaultPrivateFilePerms)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
