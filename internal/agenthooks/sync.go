package agenthooks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
)

// SyncOptions carries the machine-facing hook sync invocation: the agent to
// sync ("" = all), whether to embed, whether to detach, and the minimum
// interval between actual syncs.
type SyncOptions struct {
	Agent      string
	Embed      bool
	Background bool
	Debounce   time.Duration
}

// RunSync is the `seek hooks sync` entry point the installed stop-hooks
// execute. It suppresses sync progress so hook runners receive valid JSON on
// stdout, and records state so `seek hooks status` can report activity.
func RunSync(cfg *config.AppConfig, opts SyncOptions, stdout io.Writer) error {
	if !validHookAgent(opts.Agent) {
		fmt.Fprintf(os.Stderr, "WARN: invalid seek hook agent %q\n", opts.Agent)
		_, err := io.WriteString(stdout, "{}\n")
		return err
	}
	if opts.Background {
		return runBackground(opts, stdout)
	}
	statePath := StatePath(cfg, opts.Agent)
	lockCtx, cancelLock := context.WithTimeout(context.Background(), WriterLockTimeout)
	defer cancelLock()
	lock, err := AcquireWriterLock(lockCtx, WriterLockPath(cfg))
	if err != nil {
		latest, _ := ReadState(statePath)
		recordHookSkip(statePath, latest, "writer lock unavailable: "+err.Error())
		_, writeErr := io.WriteString(stdout, "{}\n")
		return writeErr
	}
	defer lock.Close()
	state, hasState := ReadState(statePath)
	if hasState && time.Since(state.CompletedAt) < opts.Debounce {
		recordHookSkip(statePath, state, "debounced")
		_, err := io.WriteString(stdout, "{}\n")
		return err
	}
	args := []string{"sync", "--no-lock"}
	if opts.Agent != "" {
		args = append(args, "--type", opts.Agent)
	}
	// M4: sync now embeds in the same process by default. A hook installed
	// without --embed must stay keyword-only, so pass --no-embed explicitly.
	if opts.Embed {
		// The historical hook used `seek embed --realtime`: the async batch
		// API can outlive the hook budget, so keep embedding synchronous.
		args = append(args, "--realtime")
	} else {
		args = append(args, "--no-embed")
	}
	// With --embed the single child covers sync + incremental embedding, so
	// budget the historical sync + embed windows.
	timeout := hookSyncTimeout
	if opts.Embed {
		timeout = hookSyncTimeout + hookEmbedTimeout
	}
	syncCtx, cancelSync := context.WithTimeout(context.Background(), timeout)
	defer cancelSync()
	var childErr error
	syncErr := runHooksSync(func() error {
		command := exec.CommandContext(syncCtx, hookRuntimeBinary(), args...)
		command.Env = withLockEnv(os.Environ())
		command.WaitDelay = hookContextWaitDelay
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		childErr = command.Run()
		return childErr
	}, stdout)
	now := time.Now()
	stateErr := WriteState(statePath, State{Agent: opts.Agent, CompletedAt: now, LastAttemptAt: now, Error: errorString(childErr)})
	if stateErr != nil {
		fmt.Fprintf(os.Stderr, "WARN: record seek hook state: %v\n", stateErr)
	}
	return errors.Join(syncErr, stateErr)
}

// runBackground starts the real sync in a detached process and returns before
// Codex's short Interrupt-hook timeout expires. The child runs the normal
// locked sync path and therefore remains visible through hooks status.
func runBackground(opts SyncOptions, stdout io.Writer) error {
	args := []string{"hooks", "sync", "--agent", opts.Agent}
	if opts.Embed {
		args = append(args, "--embed")
	}
	command := exec.Command(hookRuntimeBinary(), args...)
	command.Env = withoutLockEnv(os.Environ())
	// nil streams map to the OS null device. In particular, do not inherit the
	// hook runner's pipes: the detached child must not keep the hook alive.
	configureDetachedHookCommand(command)
	if err := command.Start(); err != nil {
		_, outputErr := io.WriteString(stdout, "{}\n")
		return errors.Join(fmt.Errorf("start background hook sync: %w", err), outputErr)
	}
	_ = command.Process.Release()
	_, err := io.WriteString(stdout, "{}\n")
	return err
}

func validHookAgent(agent string) bool {
	return agent == "" || agent == "claude" || agent == "codex"
}

type syncRunner func() error

func runHooksSync(sync syncRunner, output io.Writer) error {
	syncErr := sync()
	_, outputErr := io.WriteString(output, "{}\n")
	return errors.Join(syncErr, outputErr)
}
