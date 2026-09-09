// Package pipeline unifies the sync → embed → vector-index flow so callers
// (stop hooks, the service timer, `seek sync`) drive one process and one
// store handle instead of two.
//
// The historical design spawned `seek sync` and then `seek embed` as
// separate child processes, each re-opening the store and re-taking the
// lock. That left a window where freshly synced chunks were unsearchable via
// vector search, doubled WAL lock contention, and made the keyword-only
// degradation path (no embedding key configured) awkward: the embed child
// would hard-fail with an exit code while sync had already succeeded.
//
// M4 collapses this: EmbedPending runs inside the same process/store the
// indexer already used, and a missing embedding capability degrades to a
// single WARN line instead of a process failure.
package pipeline

import (
	"fmt"
	"io"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/store"
)

// Logger is the minimal output surface the pipeline needs. The command layer
// supplies os.Stdout; tests capture it.
type Logger interface {
	Printf(format string, args ...any)
}

// newline stands in for a bare fmt.Println so loggers stay one-method.

// Options controls one EmbedPending pass.
type Options struct {
	// Force re-embeds every chunk instead of only pending ones.
	Force bool
	// Realtime uses the synchronous per-batch API instead of the async
	// batch API.
	Realtime bool
	// Batch, when false, forces the realtime API (mirrors `seek embed
	// --no-batch`). Zero value is false, so callers that want the default
	// batch behaviour must set it explicitly.
	Batch bool
	// Type restricts embedding to collections of this type ("" = all).
	Type string
	// VectorIndex, when true, refreshes the HNSW index after embedding
	// (full rebuild on Force, incremental otherwise). Sync failures are
	// non-fatal: a WARN is logged and embedding is still considered done.
	VectorIndex bool
}

// EmbedPending embeds all chunks that still lack an embedding, mirroring the
// semantics of `seek embed`: pending-chunk fetch, text/image split, the
// multimodal-vs-text client decision, and the vector-index sync.
//
// It is intentionally non-fatal on per-chunk failures: a bad embedding
// response marks that chunk as skipped (its row keeps no embedding) so the
// next pass retries it, matching the legacy WARN-and-continue behaviour.
func EmbedPending(cfg *config.AppConfig, db *store.Store, opts Options, log Logger) error {
	// Keyword-only degradation, decided up front: a missing capability is a
	// configuration state, not a runtime failure, so it warns once and stops.
	if ok, why := embed.EmbeddingCapability(cfg); !ok {
		log.Printf("  skip embeddings: %s\n", why)
		return nil
	}

	var (
		chunks []store.Chunk
		err    error
	)
	if opts.Type != "" {
		chunks, err = db.GetChunksWithoutEmbeddingForCollectionType(store.CollectionType(opts.Type), opts.Force)
	} else {
		chunks, err = db.GetChunksWithoutEmbedding(opts.Force)
	}
	if err != nil {
		return fmt.Errorf("fetch pending chunks: %w", err)
	}
	if len(chunks) == 0 {
		log.Printf("All chunks already have embeddings.")
		return nil
	}

	var textChunks, imageChunks []store.Chunk
	for _, ch := range chunks {
		if ch.ChunkType == store.ChunkTypeImage {
			imageChunks = append(imageChunks, ch)
		} else {
			textChunks = append(textChunks, ch)
		}
	}
	log.Printf("Found %d chunks needing embeddings (%d text, %d image)", len(chunks), len(textChunks), len(imageChunks))

	var updated int
	if cfg.Config.Embedding.IsMultimodal() {
		vlClient := embed.NewVLClientFromConfig(cfg)
		if vlClient == nil {
			// Capability check passed but the client still failed to build
			// (e.g. a transient config race). Treat as degraded, not fatal.
			log.Printf("  skip embeddings: multimodal client unavailable — check embedding.vl_base_url")
			return nil
		}
		if len(textChunks) > 0 {
			updated += embedVLText(db, vlClient, textChunks, log)
		}
		if len(imageChunks) > 0 {
			updated += embedVLImages(db, vlClient, imageChunks, log)
		}
		log.Printf("Embedded %d/%d chunks via VL API", updated, len(chunks))
	} else {
		if len(imageChunks) > 0 {
			log.Printf("  WARNING: %d image chunks skipped (model %q does not support multimodal)", len(imageChunks), cfg.Config.Embedding.Model)
			log.Printf("  To embed images, set model to a multimodal model (e.g. qwen3-vl-embedding) or set embedding.multimodal: true")
		}
		embedClient := embed.NewClientFromConfig(cfg)
		if embedClient == nil {
			log.Printf("  skip embeddings: text client unavailable — check embedding.api_key")
			return nil
		}
		texts := make([]string, len(textChunks))
		for i, ch := range textChunks {
			texts[i] = ch.Content
		}
		if opts.Realtime || !opts.Batch {
			updated = embedRealtime(db, embedClient, textChunks, texts, log)
		} else {
			updated = embedBatch(db, embedClient, textChunks, texts, log)
		}
		log.Printf("Embedded %d/%d text chunks", updated, len(textChunks))
	}

	if opts.VectorIndex {
		log.Printf("Syncing vector index...")
		var syncErr error
		if opts.Force {
			syncErr = db.SyncVectorIndex()
		} else {
			_, syncErr = db.SyncVectorIndexIncremental()
		}
		if syncErr != nil {
			log.Printf("  WARN: vector index sync: %v", syncErr)
		}
	}

	return nil
}

func embedVLText(db *store.Store, vlClient embed.VLTextBatcher, textChunks []store.Chunk, log Logger) int {
	log.Printf("Embedding %d text chunks via VL realtime API...", len(textChunks))
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
				log.Printf("  WARN: update chunk %d: %v", textChunks[idx].ID, err)
				continue
			}
			updated++
		}
		log.Printf("\r  text: %d/%d", updated, len(textChunks))
		return nil
	})
	if err != nil {
		log.Printf("\n  WARN: text batch: %v", err)
	}
	log.Printf("\n")
	return updated
}

func embedVLImages(db *store.Store, vlClient embed.VLImageBatcher, imageChunks []store.Chunk, log Logger) int {
	log.Printf("Embedding %d image chunks via VL realtime API...", len(imageChunks))
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
				log.Printf("  WARN: update image chunk %d: %v", imageChunks[idx].ID, err)
				continue
			}
			imageUpdated++
		}
		log.Printf("\r  images: %d/%d", imageUpdated, len(imageChunks))
		return nil
	})
	if err != nil {
		log.Printf("\n  WARN: image batch: %v", err)
	}
	log.Printf("\n")
	return imageUpdated
}

func embedBatch(db *store.Store, client embed.BatchEmbedder, chunks []store.Chunk, texts []string, log Logger) int {
	log.Printf("Using Batch API (async, 50%% cheaper)...\n")
	embeddings, err := client.BatchEmbedAsync(texts, func(status string, elapsed time.Duration) {
		log.Printf("\r  [%s] %s", elapsed.Round(time.Second), status)
	})
	log.Printf("\n")
	if err != nil {
		log.Printf("  WARN: batch embed: %v", err)
		return 0
	}
	updated := 0
	for i, emb := range embeddings {
		if i < len(chunks) && emb != nil {
			if err := db.UpdateChunkEmbedding(chunks[i].ID, emb); err != nil {
				log.Printf("  WARN: update chunk %d: %v", chunks[i].ID, err)
				continue
			}
			updated++
		}
	}
	return updated
}

func embedRealtime(db *store.Store, client embed.DocumentEmbedder, chunks []store.Chunk, texts []string, log Logger) int {
	log.Printf("Using realtime API (synchronous)...\n")
	const batch = 25
	updated := 0
	for i := 0; i < len(chunks); i += batch {
		end := i + batch
		if end > len(chunks) {
			end = len(chunks)
		}
		embeddings, err := client.EmbedDocuments(texts[i:end])
		if err != nil {
			log.Printf("  WARN: batch %d-%d: %v", i, end, err)
			continue
		}
		for j, emb := range embeddings {
			idx := i + j
			if err := db.UpdateChunkEmbedding(chunks[idx].ID, emb); err != nil {
				log.Printf("  WARN: update chunk %d: %v", chunks[idx].ID, err)
				continue
			}
			updated++
		}
		log.Printf("\r  %d/%d", updated, len(chunks))
	}
	return updated
}

// stdoutLogger is the production Logger backed by a writer (io.Discard in
// hooks, os.Stdout in the CLI).
type stdoutLogger struct{ w io.Writer }

func (l stdoutLogger) Printf(format string, args ...any) {
	fmt.Fprintf(l.w, format, args...)
}

// NewStdoutLogger returns a Logger writing to w.
func NewStdoutLogger(w io.Writer) Logger { return stdoutLogger{w: w} }
