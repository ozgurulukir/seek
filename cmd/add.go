package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/seek/internal/chunk"
	"github.com/anthropics/seek/internal/config"
	"github.com/anthropics/seek/internal/embed"
	"github.com/anthropics/seek/internal/indexer"
	"github.com/anthropics/seek/internal/store"
)

type AddCmd struct {
	Path    string `arg:"" optional:"" help:"Directory to index"`
	Name    string `short:"n" help:"Name of the collection (defaults to folder name)"`
	Pattern string `short:"p" help:"Glob pattern for markdown files (default: **/*.md)"`

	Claude bool `help:"Add Claude Code conversations (~/.claude/projects/)"`
	Codex  bool `help:"Add Codex conversations (~/.codex/)"`
	Images bool `help:"Add an image directory (png/jpg/webp)"`
	Pdf    bool `help:"Add a PDF directory (rasterized for VL embedding)"`
}

func (c *AddCmd) Run(cfg *config.AppConfig) error {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

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

	col, err := db.CreateCollection(name, "markdown", absPath, c.Pattern)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (markdown) → %s\n", col.Name, col.Path)
	return indexer.New(cfg, db).SyncCollection(col)
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

	col, err := db.CreateCollection(name, "claude", path, "")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (claude) → %s\n", col.Name, col.Path)
	return indexer.New(cfg, db).SyncCollection(col)
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

	col, err := db.CreateCollection(name, "codex", path, "")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (codex) → %s\n", col.Name, col.Path)
	return indexer.New(cfg, db).SyncCollection(col)
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

	col, err := db.CreateCollection(name, "images", absPath, "**/*.{png,jpg,jpeg,webp}")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (images) → %s\n", col.Name, col.Path)
	return indexer.New(cfg, db).SyncCollection(col)
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

	col, err := db.CreateCollection(name, "pdf", absPath, "**/*.pdf")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (pdf) → %s\n", col.Name, col.Path)
	return indexer.New(cfg, db).SyncCollection(col)
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
