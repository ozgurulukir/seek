package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ozgurulukir/seek/internal/chunk"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/source/parserdef"
	"github.com/ozgurulukir/seek/internal/store"
)

type AddCmd struct {
	Path    string `arg:"" optional:"" help:"Directory to index"`
	Name    string `short:"n" help:"Name of the collection (defaults to folder name)"`
	Pattern string `short:"p" help:"Glob pattern for markdown files (default: **/*.md)"`

	Claude bool   `help:"Add Claude Code conversations (~/.claude/projects/)"`
	Codex  bool   `help:"Add Codex conversations (~/.codex/)"`
	Images bool   `help:"Add an image directory (png/jpg/webp)"`
	Pdf    bool   `help:"Add a PDF directory (rasterized for VL embedding)"`
	Code   bool   `help:"Add a source code repository directory (Go, Rust, Python, TS/JS, etc.)"`
	Parser string `help:"Add a schema-driven parser collection (e.g. opencode, copilot-cli, zed)"`

	// Documents is a universal rich-document collection (docx/xlsx/pptx/epub/
	// html/eml/csv/...). Extraction is handled by the configured backend
	// (builtin or xberg); xberg is the typical choice since it supports 100+
	// formats. See --backend and the [extractor] config section.
	Documents bool `help:"Add a documents directory via the extraction backend (docx/xlsx/pptx/epub/html/...)"`
	Docs      bool `help:"Shortcut alias for --documents"`

	// Backend overrides the extraction backend for this command (builtin|xberg).
	// Empty uses the config default (config.Extractor.Backend). Affects how PDF
	// and documents collections are extracted.
	Backend string `help:"Override the extraction backend (builtin|xberg)"`

	// Convenience aliases for the common built-in parser schemas.
	Opencode bool `help:"Shortcut for --parser opencode"`
	Copilot  bool `help:"Shortcut for --parser copilot-cli"`
	Zed      bool `help:"Shortcut for --parser zed"`

	// Schema-driven alternatives to the native parsers (text-only, no image extraction).
	ClaudeSchema bool `help:"Shortcut for --parser claude (schema-driven, text-only)"`
	CodexSchema  bool `help:"Shortcut for --parser codex (schema-driven, text-only)"`
}

func (c *AddCmd) Run(cfg *config.AppConfig) error {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	// Validate --backend early so a bad value fails before any work.
	if c.Backend != "" && c.Backend != "builtin" && c.Backend != "xberg" {
		return fmt.Errorf("invalid --backend %q (want builtin or xberg)", c.Backend)
	}

	if c.Claude {
		return c.addClaude(cfg, db)
	}
	if c.Codex {
		return c.addCodex(cfg, db)
	}
	if c.Images {
		return c.addImages(cfg, db)
	}
	if c.Pdf {
		return c.addPdfs(cfg, db)
	}
	if c.Code {
		return c.addCode(cfg, db)
	}
	if c.Documents || c.Docs {
		return c.addDocuments(cfg, db)
	}

	// Resolve convenience flags into the generic --parser path.
	// Only one parser source may be selected at a time.
	var parserFlags []string
	if c.Opencode {
		parserFlags = append(parserFlags, "--opencode")
	}
	if c.Copilot {
		parserFlags = append(parserFlags, "--copilot")
	}
	if c.Zed {
		parserFlags = append(parserFlags, "--zed")
	}
	if c.ClaudeSchema {
		parserFlags = append(parserFlags, "--claude-schema")
	}
	if c.CodexSchema {
		parserFlags = append(parserFlags, "--codex-schema")
	}
	if c.Parser != "" {
		parserFlags = append(parserFlags, "--parser")
	}
	if len(parserFlags) > 1 {
		return fmt.Errorf("multiple parser sources selected (%s); specify only one",
			strings.Join(parserFlags, ", "))
	}

	parserName := c.Parser
	if c.Opencode {
		parserName = "opencode"
	}
	if c.Copilot {
		parserName = "copilot-cli"
	}
	if c.Zed {
		parserName = "zed"
	}
	if c.ClaudeSchema {
		parserName = "claude"
	}
	if c.CodexSchema {
		parserName = "codex"
	}
	if parserName != "" {
		c.Parser = parserName
		return c.addParser(cfg, db)
	}

	return c.addMarkdown(cfg, db)
}

func (c *AddCmd) addMarkdown(cfg *config.AppConfig, db *store.Store) error {
	if c.Path == "" {
		return fmt.Errorf("path is required")
	}
	absPath, err := filepath.Abs(c.Path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	name := c.Name
	if name == "" {
		name = filepath.Base(absPath)
	}

	if c.Pattern == "" {
		c.Pattern = "**/*.md"
	}

	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d, path=%s)\n", existing.Name, existing.ID, existing.Path)
		return nil
	}

	col, err := db.CreateCollection(name, store.CollectionTypeMarkdown, absPath, c.Pattern)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (markdown) → %s\n", col.Name, col.Path)
	return c.newIndexer(cfg, db).SyncCollection(col)
}

func (c *AddCmd) addClaude(cfg *config.AppConfig, db *store.Store) error {
	name := c.Name
	if name == "" {
		name = "claude-conversations"
	}
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".claude", "projects")

	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d)\n", existing.Name, existing.ID)
		return nil
	}

	col, err := db.CreateCollection(name, store.CollectionTypeClaude, path, "")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (claude) → %s\n", col.Name, col.Path)
	return c.newIndexer(cfg, db).SyncCollection(col)
}

func (c *AddCmd) addCodex(cfg *config.AppConfig, db *store.Store) error {
	name := c.Name
	if name == "" {
		name = "codex-conversations"
	}
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".codex")

	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d)\n", existing.Name, existing.ID)
		return nil
	}

	col, err := db.CreateCollection(name, store.CollectionTypeCodex, path, "")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (codex) → %s\n", col.Name, col.Path)
	return c.newIndexer(cfg, db).SyncCollection(col)
}

func (c *AddCmd) addImages(cfg *config.AppConfig, db *store.Store) error {
	if c.Path == "" {
		return fmt.Errorf("path is required for image collection")
	}

	absPath, err := filepath.Abs(c.Path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	name := c.Name
	if name == "" {
		name = filepath.Base(absPath)
	}

	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d, path=%s)\n", existing.Name, existing.ID, existing.Path)
		return nil
	}

	col, err := db.CreateCollection(name, store.CollectionTypeImages, absPath, "**/*.{png,jpg,jpeg,webp}")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (images) → %s\n", col.Name, col.Path)
	return c.newIndexer(cfg, db).SyncCollection(col)
}

func (c *AddCmd) addPdfs(cfg *config.AppConfig, db *store.Store) error {
	if c.Path == "" {
		return fmt.Errorf("path is required for pdf collection")
	}

	absPath, err := filepath.Abs(c.Path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	name := c.Name
	if name == "" {
		name = filepath.Base(absPath)
	}

	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d, path=%s)\n", existing.Name, existing.ID, existing.Path)
		return nil
	}

	col, err := db.CreateCollection(name, store.CollectionTypePDF, absPath, "**/*.pdf")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (pdf) → %s\n", col.Name, col.Path)
	return c.newIndexer(cfg, db).SyncCollection(col)
}

// addDocuments creates a universal documents collection (docx/xlsx/pptx/epub/
// html/eml/csv/...). Extraction is delegated to the configured backend
// (builtin or xberg, overridable via --backend). For the rich formats this
// collection targets, xberg is the intended backend — warn if builtin is in
// effect, since it only handles markdown/pdf/images and will report everything
// else as unsupported.
func (c *AddCmd) addDocuments(cfg *config.AppConfig, db *store.Store) error {
	if c.Path == "" {
		return fmt.Errorf("path is required for documents collection")
	}

	absPath, err := filepath.Abs(c.Path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	name := c.Name
	if name == "" {
		name = filepath.Base(absPath)
	}

	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d, path=%s)\n", existing.Name, existing.ID, existing.Path)
		return nil
	}

	// Resolve the effective backend for the user-facing note + log line.
	// Only the explicit --backend override is persisted on the collection (an
	// empty backend means "follow the config default"), so changing the config
	// default later still applies to existing collections unless they were
	// created with an explicit --backend.
	effectiveBackend := c.Backend
	if effectiveBackend == "" {
		effectiveBackend = cfg.Config.Extractor.Backend
	}
	if effectiveBackend == "" || effectiveBackend == "builtin" {
		fmt.Printf("Note: documents collection with the builtin backend only indexes markdown/pdf/images.\n")
		fmt.Printf("      For docx/xlsx/pptx/epub/html/... use: --backend xberg (or set extractor.backend: xberg).\n")
	}

	col, err := db.CreateCollectionWithBackend(name, store.CollectionTypeDocuments, absPath, "**/*", c.Backend)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (documents, backend=%s) → %s\n", col.Name, effectiveBackend, col.Path)
	return c.newIndexer(cfg, db).SyncCollection(col)
}

func (c *AddCmd) addCode(cfg *config.AppConfig, db *store.Store) error {
	if c.Path == "" {
		return fmt.Errorf("path is required for code collection")
	}
	absPath, err := filepath.Abs(c.Path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	name := c.Name
	if name == "" {
		name = filepath.Base(absPath)
	}

	pattern := c.Pattern
	if pattern == "" {
		pattern = "**/*"
	}

	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d, path=%s)\n", existing.Name, existing.ID, existing.Path)
		return nil
	}

	col, err := db.CreateCollection(name, store.CollectionTypeCode, absPath, pattern)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (code) → %s\n", col.Name, col.Path)
	return c.newIndexer(cfg, db).SyncCollection(col)
}

func (c *AddCmd) addParser(cfg *config.AppConfig, db *store.Store) error {
	// Validate the schema exists and detects a source before creating the collection.
	// This is fail-fast: if the schema is broken or no source matches, we refuse.
	def, err := parserdef.Load(c.Parser)
	if err != nil {
		return fmt.Errorf("load parser schema: %w", err)
	}
	src, ver, files, err := def.Match()
	if err != nil {
		return fmt.Errorf("detect source for parser %q: %w", c.Parser, err)
	}

	name := c.Name
	if name == "" {
		name = c.Parser + "-conversations"
	}

	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d)\n", existing.Name, existing.ID)
		return nil
	}

	// The collection path is the primary source directory (for status display).
	primaryPath := ""
	if len(files) > 0 {
		primaryPath = filepath.Dir(files[0])
	}

	col, err := db.CreateParserCollection(name, primaryPath, "*", c.Parser)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (parser: %s v%d, %d source files) → %s\n",
		col.Name, c.Parser, ver.Version, len(files), src.Paths)
	return c.newIndexer(cfg, db).SyncCollection(col)
}

// newIndexer builds an indexer, applying the --backend override when set.
// When --backend is given, it takes precedence over both the per-collection
// backend and the config default (wired via WithExtractor). When --backend is
// empty, the indexer resolves the backend per collection (col.Backend, else the
// config default), which is what sync needs to honor each collection's recorded
// backend. All add* helpers route through here so backend selection is central.
func (c *AddCmd) newIndexer(cfg *config.AppConfig, db *store.Store) *indexer.Indexer {
	idx := indexer.New(cfg, db)
	if c.Backend == "" {
		return idx
	}
	// Build the override extractor and inject it as the explicit backend.
	ext, err := indexer.NewExtractor(cfg, c.Backend)
	if err != nil {
		// Surface the error through the sync result rather than panicking here;
		// SyncCollection will return it immediately when ext.Extract is called.
		fmt.Fprintf(os.Stderr, "WARN: --backend %s: %v\n", c.Backend, err)
		return idx
	}
	return idx.WithExtractor(ext)
}

func newEmbedClient(cfg *config.AppConfig) *embed.Client {
	key, err := cfg.RequireEmbeddingKey()
	if err != nil {
		return nil
	}
	return embed.NewClient(
		cfg.Config.Embedding.BaseURL,
		key,
		cfg.Config.Embedding.Model,
		cfg.Config.Embedding.Dimensions,
	)
}

func newVLClient(cfg *config.AppConfig) *embed.VLClient {
	key, err := cfg.RequireEmbeddingKey()
	if err != nil {
		return nil
	}
	ec := cfg.Config.Embedding
	// Only create VL client for multimodal models.
	if !ec.IsMultimodal() {
		return nil
	}
	return embed.NewVLClient(key, ec.Model, ec.Dimensions, ec.VLBaseURL)
}

// indexChunks stores chunks in DB without embeddings.
// Use `seek embed` to generate embeddings separately (batch or realtime).
func indexChunks(db *store.Store, docID int64, chunks []chunk.Chunk) error {
	for _, c := range chunks {
		if c.Type == chunk.ChunkImage {
			if err := db.InsertImageChunk(docID, c.Seq, c.Content, c.ImagePath, nil); err != nil {
				return err
			}
		} else {
			if err := db.InsertChunk(docID, c.Seq, c.Content, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// formatRelPath returns a shorter display path.
func formatRelPath(path string) string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
