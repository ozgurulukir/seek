package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Service is the single read/write/edit surface for the seek config file.
// Both the auth (credentials) and config (view/edit) command paths route
// through it so there is one place that owns config file I/O; cmd/ never reads
// or writes the config file directly.
type Service struct {
	path string
}

// NewService returns a Service rooted at the default config path
// (~/.config/seek/config.yaml). It is cheap and stateless, so callers may
// construct one per command invocation.
func NewService() *Service {
	return &Service{path: filepath.Join(configDir(), "config.yaml")}
}

// Path returns the config file path.
func (s *Service) Path() string { return s.path }

// Read returns the raw config file bytes. Callers should check Exists first to
// distinguish "not found" from a read error.
func (s *Service) Read() ([]byte, error) {
	return os.ReadFile(s.path)
}

// Exists reports whether the config file is present on disk.
func (s *Service) Exists() bool {
	_, err := os.Stat(s.path)
	return err == nil
}

// Write persists cfg to the config file (config.Save wrapper).
func (s *Service) Write(cfg Config) error {
	return Save(cfg)
}

// Edit opens the config file in the user's editor, waits for it to exit, and
// returns so the caller can reload and re-render. The editor is resolved from
// $EDITOR, then $VISUAL, then a platform default; it errors when no editor can
// be resolved so the failure is explicit rather than silently launching
// nothing.
func (s *Service) Edit() error {
	editor := ResolveEditor()
	if editor == "" {
		return fmt.Errorf("no editor found\nSet $EDITOR or $VISUAL to the command that opens your config, e.g. EDITOR=vim seek config --edit")
	}
	cmd := exec.Command(editor, s.path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("open editor %q: %w", editor, err)
	}
	return nil
}

// ResolveEditor returns the editor command to use, from $EDITOR, then
// $VISUAL, then the platform default. It returns an empty string when no
// editor can be resolved.
func ResolveEditor() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	return platformDefaultEditor()
}

// platformDefaultEditor is the fallback editor when $EDITOR and $VISUAL are
// unset. Windows has no universal console editor, so it returns "" (the caller
// then reports a clear error).
func platformDefaultEditor() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return "vim"
}
