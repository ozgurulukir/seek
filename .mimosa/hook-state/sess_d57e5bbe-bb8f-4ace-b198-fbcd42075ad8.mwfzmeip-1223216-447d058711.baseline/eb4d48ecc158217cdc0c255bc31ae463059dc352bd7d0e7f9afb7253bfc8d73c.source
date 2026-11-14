package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ozgurulukir/seek/internal/agenthooks"
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

// HooksSyncCmd is the machine-readable hook entry point. The subprocess
// contract (args, `{}\n` on stdout, exit status) lives in internal/agenthooks.
type HooksSyncCmd struct {
	Agent      string        `hidden:"" help:"Collection type to sync"`
	Embed      bool          `hidden:"" help:"Embed newly synced chunks"`
	Background bool          `hidden:"" help:"Launch sync in a detached background process"`
	Debounce   time.Duration `hidden:"" default:"15s" help:"Minimum time between hook syncs"`
}

type HooksStatusCmd struct{}

type HooksDoctorCmd struct{}

type HooksContextCmd struct {
	Agent string `hidden:""`
}

func (c *HooksInstallCmd) Run(cfg *config.AppConfig) error {
	for _, t := range agenthooks.Selected(c.Claude, c.Codex) {
		if err := agenthooks.Install(t, c.Embed, os.Stdout); err != nil {
			return err
		}
	}
	return nil
}

func (c *HooksUninstallCmd) Run(cfg *config.AppConfig) error {
	for _, t := range agenthooks.Selected(c.Claude, c.Codex) {
		if err := agenthooks.Uninstall(t, os.Stdout); err != nil {
			return err
		}
	}
	return nil
}

func (c *HooksStatusCmd) Run(cfg *config.AppConfig) error {
	var statusErr error
	for _, target := range agenthooks.Targets() {
		settings, err := agenthooks.ReadSettings(target.SettingsPath())
		if err != nil {
			fmt.Printf("%s %s: unavailable (%v)\n", target.Name(), target.Event(), err)
			statusErr = errors.Join(statusErr, err)
			continue
		}
		installed := target.Installed(settings)
		if target.IsContext() {
			fmt.Printf("%s %s: installed=%t\n", target.Name(), target.Event(), installed)
			continue
		}
		state, hasState := agenthooks.ReadState(agenthooks.StatePath(cfg, target.Agent()))
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
		fmt.Printf("%s %s: installed=%t, last sync=%s\n", target.Name(), target.Event(), installed, status)
	}
	return statusErr
}

func (c *HooksDoctorCmd) Run(cfg *config.AppConfig) error {
	var doctorErr error
	for _, target := range agenthooks.Targets() {
		settings, err := agenthooks.ReadSettings(target.SettingsPath())
		if err != nil {
			fmt.Printf("%s %s: unavailable (%v)\n", target.Name(), target.Event(), err)
			doctorErr = errors.Join(doctorErr, err)
			continue
		}
		if binary, ok := target.CommandBinary(settings); ok {
			resolved, err := agenthooks.ResolveBinary(binary)
			if err != nil {
				fmt.Printf("%s %s binary: %s (unavailable: %v)\n", target.Name(), target.Event(), binary, err)
			} else {
				fmt.Printf("%s %s binary: %s\n", target.Name(), target.Event(), resolved)
			}
		}
	}
	return errors.Join(doctorErr, (&HooksStatusCmd{}).Run(cfg))
}

func (c *HooksContextCmd) Run(cfg *config.AppConfig) error {
	return agenthooks.RunContext(c.Agent, os.Stdin, os.Stdout, os.Stderr)
}

func (c *HooksSyncCmd) Run(cfg *config.AppConfig) error {
	return agenthooks.RunSync(cfg, agenthooks.SyncOptions{
		Agent:      c.Agent,
		Embed:      c.Embed,
		Background: c.Background,
		Debounce:   c.Debounce,
	}, os.Stdout)
}
