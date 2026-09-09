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
	"context"
	"fmt"
	"io"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/indexer"
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
	// CollectionID restricts embedding to one collection. A zero value means
	// all collections, which is the behaviour of `seek embed`.
	CollectionID int64
	// SkipEmbed keeps sync useful for keyword-only runs and is used by
	// --no-embed. The index step still runs through the same Pipeline.
	SkipEmbed bool
	// VectorIndex, when true, refreshes the HNSW index after embedding
	// (full rebuild on Force, incremental otherwise). Sync failures are
	// returned after being logged so callers can report an incomplete run.
	VectorIndex bool
}

// Pipeline is the deep sync-to-embedding module. Its small interface hides
// indexer orchestration, provider capability selection and vector refresh.
type Pipeline struct {
	cfg      *config.AppConfig
	db       *store.Store
	indexer  *indexer.Indexer
	provider embed.Provider
}

func New(cfg *config.AppConfig, db *store.Store, idx *indexer.Indexer, provider embed.Provider) *Pipeline {
	return &Pipeline{cfg: cfg, db: db, indexer: idx, provider: provider}
}

// Sync indexes one collection and, unless disabled, embeds only the chunks
// produced by that collection in the same process and Store.
func (p *Pipeline) Sync(ctx context.Context, col *store.Collection, opts Options, log Logger) (indexer.SyncReport, error) {
	if p == nil || p.indexer == nil || p.db == nil {
		return indexer.SyncReport{}, fmt.Errorf("sync pipeline is not configured")
	}
	report, err := p.indexer.SyncCollectionWithReport(ctx, col)
	if err != nil {
		return report, err
	}
	if opts.CollectionID == 0 && col != nil {
		opts.CollectionID = col.ID
	}
	if opts.SkipEmbed {
		return report, nil
	}
	if err := p.embedPendingContext(ctx, opts, log); err != nil {
		return report, err
	}
	return report, nil
}

// EmbedPendingContext runs one embedding pass with the provider owned by this
// pipeline. The method is the runtime path; the package-level function below
// remains a compatibility wrapper for callers that do not construct a Runtime.
func (p *Pipeline) EmbedPendingContext(ctx context.Context, opts Options, log Logger) error {
	if p == nil {
		return fmt.Errorf("embedding pipeline is nil")
	}
	return p.embedPendingContext(ctx, opts, log)
}

// EmbedPending embeds all chunks that still lack an embedding, mirroring the
// semantics of `seek embed`: pending-chunk fetch, text/image split, the
// multimodal-vs-text client decision, and the vector-index sync.
//
// A provider or persistence failure is returned after the completed prefix;
// chunks without an embedding remain pending and can be retried safely.
func EmbedPending(cfg *config.AppConfig, db *store.Store, opts Options, log Logger) error {
	return EmbedPendingContext(context.Background(), cfg, db, opts, log)
}

// EmbedPendingContext is the runtime-owned embedding entrypoint. It keeps
// lock, network, and SQLite work cancellable while preserving the legacy
// wrapper above for direct callers.
func EmbedPendingContext(ctx context.Context, cfg *config.AppConfig, db *store.Store, opts Options, log Logger) error {
	provider, err := embed.NewProviderFromConfig(cfg)
	if err != nil {
		return err
	}
	return (&Pipeline{cfg: cfg, db: db, provider: provider}).embedPendingContext(ctx, opts, log)
}

func (p *Pipeline) embedPendingContext(ctx context.Context, opts Options, log Logger) error {
	cfg, db := p.cfg, p.db
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg == nil || db == nil {
		return fmt.Errorf("embedding pipeline is not configured")
	}
	if log == nil {
		log = stdoutLogger{w: io.Discard}
	}
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
	if opts.CollectionID != 0 {
		chunks, err = db.GetChunksWithoutEmbeddingForCollectionContext(ctx, opts.CollectionID, opts.Force)
	} else if opts.Type != "" {
		chunks, err = db.GetChunksWithoutEmbeddingForCollectionTypeContext(ctx, store.CollectionType(opts.Type), opts.Force)
	} else {
		chunks, err = db.GetChunksWithoutEmbeddingContext(ctx, opts.Force)
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
		if err := ctx.Err(); err != nil {
			return err
		}
		if ch.ChunkType == store.ChunkTypeImage {
			imageChunks = append(imageChunks, ch)
		} else {
			textChunks = append(textChunks, ch)
		}
	}
	log.Printf("Found %d chunks needing embeddings (%d text, %d image)", len(chunks), len(textChunks), len(imageChunks))

	var updated int
	if cfg.Config.Embedding.IsMultimodal() {
		vlClient := p.provider.VLQuery
		if vlClient == nil {
			return fmt.Errorf("multimodal embedding client unavailable — check embedding.vl_base_url")
		}
		if len(textChunks) > 0 {
			if p.provider.VLText == nil {
				return fmt.Errorf("multimodal text embedding capability unavailable")
			}
			count, err := embedVLTextContext(ctx, db, p.provider.VLText, textChunks, log)
			updated += count
			if err != nil {
				return err
			}
		}
		if len(imageChunks) > 0 {
			if p.provider.VLImage == nil {
				return fmt.Errorf("multimodal image embedding capability unavailable")
			}
			count, err := embedVLImagesContext(ctx, db, p.provider.VLImage, imageChunks, log)
			updated += count
			if err != nil {
				return err
			}
		}
		log.Printf("Embedded %d/%d chunks via VL API", updated, len(chunks))
	} else {
		if len(imageChunks) > 0 {
			log.Printf("  WARNING: %d image chunks skipped (model %q does not support multimodal)", len(imageChunks), cfg.Config.Embedding.Model)
			log.Printf("  To embed images, set model to a multimodal model (e.g. qwen3-vl-embedding) or set embedding.multimodal: true")
		}
		embedClient := p.provider.Document
		if embedClient == nil {
			return fmt.Errorf("text embedding client unavailable — check embedding.api_key")
		}
		// Nothing to embed text-wise (only image chunks, non-multimodal):
		// skip the embed call entirely rather than round-trip an empty batch.
		if len(textChunks) > 0 {
			texts := make([]string, len(textChunks))
			for i, ch := range textChunks {
				texts[i] = ch.Content
			}
			if opts.Realtime || !opts.Batch {
				updated, err = embedRealtimeContext(ctx, db, embedClient, textChunks, texts, log)
			} else {
				updated, err = embedBatchContext(ctx, db, p.provider.Batch, textChunks, texts, log)
			}
			if err != nil {
				return err
			}
			log.Printf("Embedded %d/%d text chunks", updated, len(textChunks))
		}
	}

	if opts.VectorIndex {
		log.Printf("Syncing vector index...")
		var syncErr error
		if opts.Force {
			syncErr = db.SyncVectorIndexContext(ctx)
		} else {
			_, syncErr = db.SyncVectorIndexIncrementalContext(ctx)
		}
		if syncErr != nil {
			log.Printf("  WARN: vector index sync: %v", syncErr)
			return fmt.Errorf("vector index sync: %w", syncErr)
		}
	}

	return nil
}

func embedVLText(db *store.Store, vlClient embed.VLTextBatcher, textChunks []store.Chunk, log Logger) (int, error) {
	return embedVLTextContext(context.Background(), db, vlClient, textChunks, log)
}

func embedVLTextContext(ctx context.Context, db *store.Store, vlClient embed.VLTextBatcher, textChunks []store.Chunk, log Logger) (int, error) {
	log.Printf("Embedding %d text chunks via VL realtime API...", len(textChunks))
	texts := make([]string, len(textChunks))
	for i, ch := range textChunks {
		texts[i] = ch.Content
	}
	updated := 0
	var err error
	if batcher, ok := vlClient.(embed.ContextVLTextBatcher); ok {
		_, err = batcher.EmbedTextsInBatchesContext(ctx, texts, 20, 200*time.Millisecond, func(batchStart int, embeddings [][]float32) error {
			return updateTextEmbeddings(ctx, db, textChunks, batchStart, embeddings, &updated, log)
		})
	} else {
		_, err = vlClient.EmbedTextsInBatches(texts, 20, 200*time.Millisecond, func(batchStart int, embeddings [][]float32) error {
			return updateTextEmbeddings(ctx, db, textChunks, batchStart, embeddings, &updated, log)
		})
	}
	if err != nil {
		return updated, fmt.Errorf("text batch: %w", err)
	}
	log.Printf("\n")
	return updated, nil
}

func updateTextEmbeddings(ctx context.Context, db *store.Store, chunks []store.Chunk, batchStart int, embeddings [][]float32, updated *int, log Logger) error {
	for j, emb := range embeddings {
		if err := ctx.Err(); err != nil {
			return err
		}
		idx := batchStart + j
		if emb == nil || idx >= len(chunks) {
			continue
		}
		if err := db.UpdateChunkEmbeddingContext(ctx, chunks[idx].ID, emb); err != nil {
			return fmt.Errorf("update chunk %d: %w", chunks[idx].ID, err)
		}
		*updated++
	}
	log.Printf("\r  text: %d/%d", *updated, len(chunks))
	return nil
}

func embedVLImages(db *store.Store, vlClient embed.VLImageBatcher, imageChunks []store.Chunk, log Logger) (int, error) {
	return embedVLImagesContext(context.Background(), db, vlClient, imageChunks, log)
}

func embedVLImagesContext(ctx context.Context, db *store.Store, vlClient embed.VLImageBatcher, imageChunks []store.Chunk, log Logger) (int, error) {
	log.Printf("Embedding %d image chunks via VL realtime API...", len(imageChunks))
	items := make([]embed.ImageBatchItem, len(imageChunks))
	for i, ch := range imageChunks {
		items[i] = embed.ImageBatchItem{ImagePath: ch.ImagePath, Text: ch.Content}
	}
	imageUpdated := 0
	var err error
	callback := func(batchStart int, embeddings [][]float32, validIndices []int) error {
		for j, emb := range embeddings {
			if err := ctx.Err(); err != nil {
				return err
			}
			if emb == nil || j >= len(validIndices) {
				continue
			}
			idx := validIndices[j]
			if err := db.UpdateChunkEmbeddingContext(ctx, imageChunks[idx].ID, emb); err != nil {
				return fmt.Errorf("update image chunk %d: %w", imageChunks[idx].ID, err)
			}
			imageUpdated++
		}
		log.Printf("\r  images: %d/%d", imageUpdated, len(imageChunks))
		return nil
	}
	if batcher, ok := vlClient.(embed.ContextVLImageBatcher); ok {
		_, err = batcher.EmbedImagesInBatchesContext(ctx, items, 5, 500*time.Millisecond, callback)
	} else {
		_, err = vlClient.EmbedImagesInBatches(items, 5, 500*time.Millisecond, callback)
	}
	if err != nil {
		return imageUpdated, fmt.Errorf("image batch: %w", err)
	}
	log.Printf("\n")
	return imageUpdated, nil
}

func embedBatch(db *store.Store, client embed.BatchEmbedder, chunks []store.Chunk, texts []string, log Logger) (int, error) {
	return embedBatchContext(context.Background(), db, client, chunks, texts, log)
}

func embedBatchContext(ctx context.Context, db *store.Store, client embed.BatchEmbedder, chunks []store.Chunk, texts []string, log Logger) (int, error) {
	if client == nil {
		return 0, fmt.Errorf("batch embedding client is unavailable")
	}
	log.Printf("Using Batch API (async, 50%% cheaper)...\n")
	var embeddings [][]float32
	var err error
	progress := func(status string, elapsed time.Duration) {
		log.Printf("\r  [%s] %s", elapsed.Round(time.Second), status)
	}
	if batcher, ok := client.(embed.ContextBatchEmbedder); ok {
		embeddings, err = batcher.BatchEmbedAsyncContext(ctx, texts, progress)
	} else {
		embeddings, err = client.BatchEmbedAsync(texts, progress)
	}
	log.Printf("\n")
	if err != nil {
		return 0, fmt.Errorf("batch embed: %w", err)
	}
	updated := 0
	for i, emb := range embeddings {
		if i < len(chunks) && emb != nil {
			if err := db.UpdateChunkEmbeddingContext(ctx, chunks[i].ID, emb); err != nil {
				return updated, fmt.Errorf("update chunk %d: %w", chunks[i].ID, err)
			}
			updated++
		}
	}
	return updated, nil
}

func embedRealtime(db *store.Store, client embed.DocumentEmbedder, chunks []store.Chunk, texts []string, log Logger) (int, error) {
	return embedRealtimeContext(context.Background(), db, client, chunks, texts, log)
}

func embedRealtimeContext(ctx context.Context, db *store.Store, client embed.DocumentEmbedder, chunks []store.Chunk, texts []string, log Logger) (int, error) {
	if client == nil {
		return 0, fmt.Errorf("document embedding client is unavailable")
	}
	log.Printf("Using realtime API (synchronous)...\n")
	const batch = 25
	updated := 0
	for i := 0; i < len(chunks); i += batch {
		if err := ctx.Err(); err != nil {
			return updated, err
		}
		end := i + batch
		if end > len(chunks) {
			end = len(chunks)
		}
		var embeddings [][]float32
		var err error
		if contextClient, ok := client.(embed.ContextDocumentEmbedder); ok {
			embeddings, err = contextClient.EmbedDocumentsContext(ctx, texts[i:end])
		} else {
			embeddings, err = client.EmbedDocuments(texts[i:end])
		}
		if err != nil {
			return updated, fmt.Errorf("batch %d-%d: %w", i, end, err)
		}
		for j, emb := range embeddings {
			idx := i + j
			if idx >= len(chunks) {
				break
			}
			if err := db.UpdateChunkEmbeddingContext(ctx, chunks[idx].ID, emb); err != nil {
				return updated, fmt.Errorf("update chunk %d: %w", chunks[idx].ID, err)
			}
			updated++
		}
		log.Printf("\r  %d/%d", updated, len(chunks))
	}
	return updated, nil
}

// stdoutLogger is the production Logger backed by a writer (io.Discard in
// hooks, os.Stdout in the CLI).
type stdoutLogger struct{ w io.Writer }

func (l stdoutLogger) Printf(format string, args ...any) {
	fmt.Fprintf(l.w, format, args...)
}

// NewStdoutLogger returns a Logger writing to w.
func NewStdoutLogger(w io.Writer) Logger { return stdoutLogger{w: w} }
