package cmd

import (
	"fmt"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/store"
)

type EmbedCmd struct {
	Force    bool `short:"f" help:"Force re-embed all chunks"`
	Batch    bool `short:"b" default:"true" help:"Use batch API (50% cheaper, async) — text-only models"`
	Realtime bool `short:"r" help:"Use realtime API (synchronous, immediate)"`
}

func (c *EmbedCmd) Run(cfg *config.AppConfig) error {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	// Set up vector index if configured
	vectorIndex := false
	if cfg.Config.VectorIndex.Backend != "" && cfg.Config.VectorIndex.Backend != "linear" {
		vi, err := store.NewVectorIndex(cfg)
		if err == nil {
			db.SetVectorIndex(vi)
			vectorIndex = true
			defer vi.Save(cfg.Config.VectorIndex.HNSW.PersistPath)
		}
	}

	// Get chunks that need embedding
	chunks, err := db.GetChunksWithoutEmbedding(c.Force)
	if err != nil {
		return fmt.Errorf("get chunks: %w", err)
	}

	if len(chunks) == 0 {
		fmt.Println("All chunks already have embeddings.")
		return nil
	}

	// Separate text and image chunks
	var textChunks, imageChunks []store.Chunk
	for _, ch := range chunks {
		if ch.ChunkType == store.ChunkTypeImage {
			imageChunks = append(imageChunks, ch)
		} else {
			textChunks = append(textChunks, ch)
		}
	}

	fmt.Printf("Found %d chunks needing embeddings (%d text, %d image)\n", len(chunks), len(textChunks), len(imageChunks))

	// Decide which client to use based on model
	if cfg.Config.Embedding.IsMultimodal() {
		if err := c.embedWithVL(cfg, db, textChunks, imageChunks); err != nil {
			return err
		}
		if vectorIndex {
			fmt.Println("Syncing vector index...")
			var syncErr error
			if c.Force {
				syncErr = db.SyncVectorIndex()
			} else {
				_, syncErr = db.SyncVectorIndexIncremental()
			}
			if syncErr != nil {
				fmt.Printf("  WARN: vector index sync: %v\n", syncErr)
			}
		}
		return nil
	} else if len(imageChunks) > 0 {
		fmt.Printf("  WARNING: %d image chunks skipped (model %q does not support multimodal)\n", len(imageChunks), cfg.Config.Embedding.Model)
		fmt.Println("  To embed images, set model to qwen3-vl-embedding in config")
	}

	embedClient := newEmbedClient(cfg)
	if embedClient == nil {
		return fmt.Errorf("embedding API key not configured. Run: seek auth login")
	}

	texts := make([]string, len(textChunks))
	for i, ch := range textChunks {
		texts[i] = ch.Content
	}

	if c.Realtime || !c.Batch {
		if err := c.embedRealtime(db, embedClient, textChunks, texts); err != nil {
			return err
		}
	} else {
		if err := c.embedBatch(db, embedClient, textChunks, texts); err != nil {
			return err
		}
	}

	// Sync HNSW index with newly embedded chunks
	if vectorIndex {
		fmt.Println("Syncing vector index...")
		var syncErr error
		if c.Force {
			syncErr = db.SyncVectorIndex()
		} else {
			_, syncErr = db.SyncVectorIndexIncremental()
		}
		if syncErr != nil {
			fmt.Printf("  WARN: vector index sync: %v\n", syncErr)
		}
	}

	return nil
}

// embedWithVL uses the VL realtime API for all chunks (unified vector space).
func (c *EmbedCmd) embedWithVL(cfg *config.AppConfig, db *store.Store, textChunks, imageChunks []store.Chunk) error {
	vlClient := newVLClient(cfg)
	if vlClient == nil {
		return fmt.Errorf("embedding API key not configured. Run: seek auth login")
	}

	updated := 0
	total := len(textChunks) + len(imageChunks)

	// 1. Embed text chunks in batches of 20 via VL API
	if len(textChunks) > 0 {
		updated += c.embedVLTextChunks(db, vlClient, textChunks)
	}

	// 2. Embed image chunks in batches of 5 via VL API
	if len(imageChunks) > 0 {
		updated += c.embedVLImageChunks(db, vlClient, imageChunks)
	}

	fmt.Printf("Embedded %d/%d chunks via VL API\n", updated, total)
	return nil
}

func (c *EmbedCmd) embedVLTextChunks(db *store.Store, vlClient *embed.VLClient, textChunks []store.Chunk) int {
	fmt.Printf("Embedding %d text chunks via VL realtime API...\n", len(textChunks))

	texts := make([]string, len(textChunks))
	for i, ch := range textChunks {
		texts[i] = ch.Content
	}

	updated := 0
	_, err := vlClient.EmbedTextsInBatches(texts, 20, 200*time.Millisecond, func(batchStart int, embeddings [][]float32) error {
		for j, emb := range embeddings {
			idx := batchStart + j
			if emb == nil {
				continue
			}
			if err := db.UpdateChunkEmbedding(textChunks[idx].ID, emb); err != nil {
				fmt.Printf("  WARN: update chunk %d: %v\n", textChunks[idx].ID, err)
				continue
			}
			updated++
		}
		fmt.Printf("\r  text: %d/%d", updated, len(textChunks))
		return nil
	})
	if err != nil {
		fmt.Printf("\n  WARN: text batch: %v\n", err)
	}

	fmt.Println()
	return updated
}

func (c *EmbedCmd) embedVLImageChunks(db *store.Store, vlClient *embed.VLClient, imageChunks []store.Chunk) int {
	fmt.Printf("Embedding %d image chunks via VL realtime API...\n", len(imageChunks))

	items := make([]embed.ImageBatchItem, len(imageChunks))
	for i, ch := range imageChunks {
		items[i] = embed.ImageBatchItem{ImagePath: ch.ImagePath, Text: ch.Content}
	}

	imageUpdated := 0
	_, err := vlClient.EmbedImagesInBatches(items, 5, 500*time.Millisecond, func(batchStart int, embeddings [][]float32, validIndices []int) error {
		for j, emb := range embeddings {
			if emb == nil || j >= len(validIndices) {
				continue
			}
			idx := validIndices[j]
			if err := db.UpdateChunkEmbedding(imageChunks[idx].ID, emb); err != nil {
				fmt.Printf("  WARN: update image chunk %d: %v\n", imageChunks[idx].ID, err)
				continue
			}
			imageUpdated++
		}
		fmt.Printf("\r  images: %d/%d", imageUpdated, len(imageChunks))
		return nil
	})
	if err != nil {
		fmt.Printf("\n  WARN: image batch: %v\n", err)
	}

	fmt.Println()
	return imageUpdated
}

func (c *EmbedCmd) embedBatch(db *store.Store, client *embed.Client, chunks []store.Chunk, texts []string) error {
	fmt.Printf("Using Batch API (async, 50%% cheaper)...\n\n")

	embeddings, err := client.BatchEmbedAsync(texts, func(status string, elapsed time.Duration) {
		fmt.Printf("\r  [%s] %s", elapsed.Round(time.Second), status)
	})
	fmt.Println() // newline after status updates

	if err != nil {
		return fmt.Errorf("batch embed: %w", err)
	}

	// Write embeddings back to DB
	updated := 0
	for i, ch := range chunks {
		if i < len(embeddings) && embeddings[i] != nil {
			if err := db.UpdateChunkEmbedding(ch.ID, embeddings[i]); err != nil {
				fmt.Printf("  WARN: update chunk %d: %v\n", ch.ID, err)
				continue
			}
			updated++
		}
	}

	fmt.Printf("Embedded %d/%d chunks via batch\n", updated, len(chunks))
	return nil
}

func (c *EmbedCmd) embedRealtime(db *store.Store, client *embed.Client, chunks []store.Chunk, texts []string) error {
	fmt.Printf("Using realtime API...\n")

	const batchSize = 6
	updated := 0

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		embeddings, err := client.EmbedDocuments(texts[i:end])
		if err != nil {
			fmt.Printf("  WARN: batch %d-%d: %v\n", i, end, err)
			continue
		}

		for j, emb := range embeddings {
			idx := i + j
			if err := db.UpdateChunkEmbedding(chunks[idx].ID, emb); err != nil {
				fmt.Printf("  WARN: update chunk %d: %v\n", chunks[idx].ID, err)
				continue
			}
			updated++
		}

		fmt.Printf("\r  %d/%d", updated, len(chunks))
	}

	fmt.Printf("\nEmbedded %d/%d chunks\n", updated, len(chunks))
	return nil
}
