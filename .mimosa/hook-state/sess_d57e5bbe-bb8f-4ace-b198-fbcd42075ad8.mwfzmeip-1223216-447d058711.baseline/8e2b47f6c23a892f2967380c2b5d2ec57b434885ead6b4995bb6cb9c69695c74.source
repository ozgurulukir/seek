package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ozgurulukir/seek/internal/agenthooks"
	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
)

// AddCmd is the `seek add` surface (plan §3.1). The canonical selectors are
// --type and --agent; the native (--claude/--codex/...) and parser
// (--opencode/.../--parser/...) flags are kept as compatibility aliases. Every
// combination is validated and mapped to a single app.AddRequest by
// app.BuildAddRequest, then executed by app.AddCollectionService — there is no
// per-type add path here.
type AddCmd struct {
	Path    string `arg:"" optional:"" help:"Directory to index"`
	Name    string `short:"n" help:"Name of the collection (defaults to folder name)"`
	Pattern string `short:"p" help:"Glob pattern for markdown files (default: **/*.md)"`

	// Canonical selectors: exactly one collection kind may be chosen.
	Type  string `help:"Collection type: markdown|code|documents|pdf|images"`
	Agent string `help:"Agent: claude|codex|opencode|copilot|zed|hermes (claude/codex are native)"`

	// Native selectors (compatibility aliases of --type/--agent).
	Claude    bool `help:"Add Claude Code conversations (~/.claude/projects/)"`
	Codex     bool `help:"Add Codex conversations (~/.codex/)"`
	Images    bool `help:"Add an image directory (png/jpg/webp)"`
	Pdf       bool `help:"Add a PDF directory (rasterized for VL embedding)"`
	Code      bool `help:"Add a source code repository directory (Go, Rust, Python, TS/JS, etc)"`
	Documents bool `help:"Add a documents directory via the extraction backend (docx/xlsx/pptx/epub/html/...)"`
	Docs      bool `help:"Shortcut alias for --documents"`

	// Parser collections (schema-driven). --agent opencode|copilot|zed|hermes
	// are aliases for the matching --parser schema name.
	Parser string `help:"Add a schema-driven parser collection (e.g. opencode, copilot-cli, zed)"`

	Opencode     bool `help:"Shortcut for --parser opencode"`
	Copilot      bool `help:"Shortcut for --parser copilot-cli"`
	Zed          bool `help:"Shortcut for --parser zed"`
	Hermes       bool `help:"Shortcut for --parser hermes (Hermes Agent state.db)"`
	ClaudeSchema bool `help:"Shortcut for --parser claude (schema-driven, text-only)"`
	CodexSchema  bool `help:"Shortcut for --parser codex (schema-driven, text-only)"`

	// Backend overrides the extraction backend for this command (builtin|xberg).
	// Empty uses the config default. Affects how PDF and documents collections
	// are extracted. See --backend and the [extractor] config section.
	Backend string `help:"Override the extraction backend (builtin|xberg)"`
}

func (c *AddCmd) Run(cfg *config.AppConfig) (err error) {
	// Validate and map the flags to a single request before touching the store
	// or acquiring the writer lock, so a bad selection fails fast.
	req, err := app.BuildAddRequest(c.toFlags())
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), agenthooks.WriterLockTimeout)
	defer cancel()
	lock, err := agenthooks.AcquireWriterLock(ctx, agenthooks.WriterLockPath(cfg))
	if err != nil {
		return fmt.Errorf("acquire writer lock: %w", err)
	}
	defer lock.Close()

	db, err := app.OpenStore(cfg)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	return app.NewAddCollectionService(db, cfg).Add(req)
}

// toFlags bridges the kong struct to app.AddFlags (app must not import cmd).
func (c *AddCmd) toFlags() app.AddFlags {
	return app.AddFlags{
		Path:         c.Path,
		Name:         c.Name,
		Pattern:      c.Pattern,
		Type:         c.Type,
		Agent:        c.Agent,
		Parser:       c.Parser,
		Backend:      c.Backend,
		Claude:       c.Claude,
		Codex:        c.Codex,
		Images:       c.Images,
		Pdf:          c.Pdf,
		Code:         c.Code,
		Documents:    c.Documents,
		Docs:         c.Docs,
		Opencode:     c.Opencode,
		Copilot:      c.Copilot,
		Zed:          c.Zed,
		Hermes:       c.Hermes,
		ClaudeSchema: c.ClaudeSchema,
		CodexSchema:  c.CodexSchema,
	}
}

// formatRelPath returns a shorter display path.
func formatRelPath(path string) string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
