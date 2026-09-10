package indexer

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ozgurulukir/seek/internal/chunk"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/extractor"
	"github.com/ozgurulukir/seek/internal/extractor/builtin"
	"github.com/ozgurulukir/seek/internal/extractor/xberg"
	"github.com/ozgurulukir/seek/internal/semantic"
	"github.com/ozgurulukir/seek/internal/source"
	"github.com/ozgurulukir/seek/internal/store"
)

// Logger allows the indexer to emit progress without hardcoding fmt.Printf
type Logger interface {
	Printf(format string, v ...interface{})
}

type defaultLogger struct{}

func (l defaultLogger) Printf(format string, v ...interface{}) {
	fmt.Printf(format, v...)
}

type Indexer struct {
	cfg      *config.AppConfig
	db       *store.Store
	writer   IndexWriter
	log      Logger
	ctxValue context.Context
	report   SyncReport
	// ext is an explicit override for the extraction backend, taking precedence
	// over both per-collection backend and the config default. Set via
	// WithExtractor (e.g. from a --backend flag). When nil, the backend is
	// resolved per collection (see extractorFor).
	ext      extractor.Extractor
	resolver ExtractorResolver
	// sem* cache the resolved semantic provider. Resolution (offline gate +
	// one health check) happens at most once per indexer instance; handlers
	// run sequentially.
	semChecked bool
	semClient  semantic.Provider
}

func New(cfg *config.AppConfig, db *store.Store) *Indexer {
	return NewWithDependencies(cfg, db, NewConfigExtractorResolver(cfg), db)
}

// NewWithDependencies is the composition-root constructor. The legacy New
// convenience constructor remains for package callers and tests, while the
// runtime supplies extractor and writer dependencies explicitly.
func NewWithDependencies(cfg *config.AppConfig, db *store.Store, resolver ExtractorResolver, writer IndexWriter) *Indexer {
	return &Indexer{
		cfg:      cfg,
		db:       db,
		writer:   writer,
		log:      defaultLogger{},
		ctxValue: context.Background(),
		resolver: resolver,
	}
}

// WithExtractor overrides the extraction backend for all collections (e.g. from
// a --backend flag). Pass nil to revert to per-collection / config resolution.
func (idx *Indexer) WithExtractor(ext extractor.Extractor) *Indexer {
	idx.ext = ext
	return idx
}

// WithExtractorResolver injects backend construction so the indexer does not
// need to know how the composition root obtains extractors.
func (idx *Indexer) WithExtractorResolver(resolver ExtractorResolver) *Indexer {
	idx.resolver = resolver
	return idx
}

// WithIndexWriter injects the transactional persistence seam used by all
// format handlers. The default writer is the Store passed to New.
func (idx *Indexer) WithIndexWriter(writer IndexWriter) *Indexer {
	idx.writer = writer
	return idx
}

func (idx *Indexer) chunkSize() (int, int) {
	if idx.cfg == nil {
		return 0, 0
	}
	return idx.cfg.Config.Chunk.MaxSize, idx.cfg.Config.Chunk.Overlap
}

// extractorFor resolves the extractor to use for a collection. Precedence:
//  1. idx.ext (explicit override, e.g. --backend flag);
//  2. col.Backend (per-collection override persisted at add time);
//  3. cfg.Config.Extractor.Backend (global config default).
//
// Empty backend strings fall through to the configured resolver default.
func (idx *Indexer) extractorFor(col *store.Collection) (extractor.Extractor, error) {
	if idx.ext != nil {
		return idx.ext, nil
	}
	if idx.resolver == nil {
		return nil, fmt.Errorf("extractor resolver is not configured")
	}
	return idx.resolver.Resolve(col)
}

// NewExtractor builds the extractor named by backend. An empty backend selects
// the config default. It lives here (not in the extractor package) to avoid an
// import cycle: both backends import extractor for the interface, so the
// constructor that picks between them must sit above them. For xberg it uses
// NewWithHealthCheck so an unreachable server fails fast at sync start rather
// than silently marking every file unsupported.
func NewExtractor(cfg *config.AppConfig, backend string) (extractor.Extractor, error) {
	if backend == "" {
		backend = cfg.Config.Extractor.Backend
	}
	var ocr extractor.OCR
	if cfg.Config.CanUseOCR() {
		ocr = embed.NewOCRClient(cfg.Config.OCR.BaseURL, cfg.Config.OCR.APIKey, cfg.Config.OCR.Model, cfg.Config.OCR.MaxTokens)
	}
	switch backend {
	case "", "builtin":
		return builtin.New(ocr, cfg.CacheDir), nil
	case "xberg":
		// The xberg backend extracts rich documents by POSTing their contents
		// to the xberg HTTP service — external data flow even when that
		// service runs on localhost. privacy.offline_only forbids every
		// network call, so refuse the backend outright rather than silently
		// shipping document text over the wire (review finding: the offline
		// guarantee previously did not cover this path).
		if cfg.Config.OfflineOnly() {
			return nil, fmt.Errorf("offline_only is enabled: the xberg extractor sends document contents to %q; use --backend builtin (or disable offline_only)", func() string {
				if u := cfg.Config.Extractor.XbergBaseURL; u != "" {
					return u
				}
				return config.DefaultXbergBaseURL
			}())
		}
		return xberg.NewWithHealthCheck(cfg.Config.Extractor, cfg.CacheDir)
	default:
		return nil, fmt.Errorf("unknown extractor backend %q (want builtin or xberg)", backend)
	}
}

// WithContext attaches the caller-owned cancellation context to disk,
// extraction, and persistence work performed by this indexer.
func (idx *Indexer) WithContext(ctx context.Context) *Indexer {
	if ctx == nil {
		ctx = context.Background()
	}
	idx.ctxValue = ctx
	return idx
}

func (idx *Indexer) ctx() context.Context {
	if idx.ctxValue == nil {
		return context.Background()
	}
	return idx.ctxValue
}

// writeFastFields is retained for package-level helpers and older callers;
// production document paths use DocumentIndex.FastFields so metadata commits
// in the same transaction as the document and chunks.
func (idx *Indexer) writeFastFields(docID int64, label string, metadata map[string]string) {
	for field, value := range metadata {
		if value == "" {
			continue
		}
		if err := idx.db.FastFields().Set(docID, field, value); err != nil {
			if label != "" {
				idx.warnf("  WARN: fastfield %s=%s %s: %v\n", field, value, label, err)
			} else {
				idx.warnf("  WARN: metadata %s=%s: %v\n", field, value, err)
			}
		}
	}
}

func (idx *Indexer) WithLogger(l Logger) *Indexer {
	idx.log = l
	return idx
}

func (idx *Indexer) warnf(format string, v ...interface{}) {
	idx.report.Warnings++
	idx.log.Printf(format, v...)
}

func (idx *Indexer) addReport(report SyncReport) {
	idx.report.add(report)
}

func (idx *Indexer) recordFailure(path, kind string, err error) {
	if err == nil {
		return
	}
	idx.report.Errors = append(idx.report.Errors, SyncFailure{Path: path, Kind: kind, Error: err.Error()})
}

// LastReport returns a snapshot of the most recent collection sync report.
func (idx *Indexer) LastReport() SyncReport {
	report := idx.report
	report.Errors = append([]SyncFailure(nil), report.Errors...)
	return report
}

func (idx *Indexer) SyncCollection(col *store.Collection) error {
	return idx.SyncCollectionContext(context.Background(), col)
}

// SyncCollectionContext syncs one collection with the supplied cancellation
// context. The compatibility method above keeps existing command/test call
// sites source-compatible during the runtime migration.
func (idx *Indexer) SyncCollectionContext(ctx context.Context, col *store.Collection) error {
	_, err := idx.SyncCollectionWithReport(ctx, col)
	return err
}

// SyncCollectionWithReport syncs one collection and returns structured
// accounting alongside the compatibility error result.
func (idx *Indexer) SyncCollectionWithReport(ctx context.Context, col *store.Collection) (SyncReport, error) {
	idx.report = SyncReport{}
	if col == nil {
		idx.report.Failed++
		idx.report.Errors = append(idx.report.Errors, SyncFailure{Kind: "invalid_collection", Error: "nil collection"})
		return idx.LastReport(), fmt.Errorf("sync collection: nil collection")
	}
	idx.report.Collection = col.Name
	idx.WithContext(ctx)
	h, ok := syncHandlers[col.Type]
	if !ok {
		idx.report.Unsupported++
		idx.report.Errors = append(idx.report.Errors, SyncFailure{Kind: "unsupported_collection", Error: fmt.Sprintf("unknown collection type: %s", col.Type)})
		return idx.LastReport(), fmt.Errorf("unknown collection type: %s", col.Type)
	}
	err := h.Sync(idx.ctx(), &HandlerDeps{Indexer: idx, Writer: idx.writer}, col)
	if err != nil {
		idx.report.Failed++
		idx.report.Errors = append(idx.report.Errors, SyncFailure{Kind: "collection", Error: err.Error()})
	}
	return idx.LastReport(), err
}

// ConversationBatch is the normalized output of Claude/Codex parsers. The
// format-specific message types stop at the parser adapter; the shared sync
// path only handles the text, title, session identity, and saved images it
// needs to persist.
type ConversationBatch struct {
	Text      string
	Title     string
	SessionID string
	Images    []source.ConversationImage
}

// syncConversation abstracts the heavily duplicated logic between Claude and Codex
func (idx *Indexer) syncConversation(
	col *store.Collection,
	scanFiles func() ([]source.ConversationFile, error),
	parseFile func(path string, fromLine int) (ConversationBatch, error),
	getTitle func(sessionID, defaultTitle string) string,
) error {
	files, err := scanFiles()
	if err != nil {
		return err
	}

	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	if _, err := idx.cleanupOrphans(col.ID, diskPaths, "conversations"); err != nil {
		return fmt.Errorf("cleanup conversations: %w", err)
	}

	var indexed, skipped, totalImages, failed int

	for _, f := range files {
		if err := idx.ctx().Err(); err != nil {
			return err
		}
		existing, err := idx.db.GetDocumentContext(idx.ctx(), col.ID, f.Path)
		if err == nil && existing.Mtime >= f.Mtime {
			skipped++
			continue
		}

		lineCount, err := source.CountLinesContext(idx.ctx(), f.Path)
		if err != nil {
			idx.warnf("  WARN: count lines %s: %v\n", f.Path, err)
			failed++
			continue
		}
		// Conversation files are expected to be append-only: a file that
		// shrank (or has the same line count) was truncated or edited in
		// place, so the incremental path below cannot reconstruct its
		// content from the delta. Fall through to a full re-parse
		// (fromLine = 0), which replaces FTS and re-chunks the whole file,
		// keeping the index in sync with what is actually on disk.
		fromLine := 0
		if existing != nil && existing.LineCount < lineCount {
			fromLine = existing.LineCount
		}

		batch, err := parseFile(f.Path, fromLine)
		if err != nil {
			idx.warnf("  WARN: parse %s: %v\n", f.Path, err)
			failed++
			continue
		}

		if batch.Text == "" && len(batch.Images) == 0 {
			if existing != nil {
				if fromLine == 0 {
					// A full re-parse that yields nothing means the file no
					// longer contains parseable content (e.g. truncated to
					// empty or to metadata-only lines). Remove the stale
					// document so its FTS entry and chunks go with it,
					// mirroring deleted files.
					if err := idx.db.DeleteDocumentContext(idx.ctx(), existing.ID); err != nil {
						return fmt.Errorf("delete empty document %s: %w", f.Path, err)
					}
				} else {
					// Append that produced no new content: just record the
					// mtime so subsequent syncs skip this file without
					// re-parsing it.
					if err := idx.db.UpdateDocumentMtimeContext(idx.ctx(), existing.ID, f.Mtime); err != nil {
						return fmt.Errorf("update mtime %s: %w", f.Path, err)
					}
				}
			}
			skipped++
			continue
		}

		title := filepath.Base(f.Path)
		if fromLine == 0 && batch.Title != "" {
			title = batch.Title
		}
		if getTitle != nil {
			title = getTitle(batch.SessionID, title)
		}

		// On an append, new chunks must continue after the seqs from the
		// previous sync and their line spans must be offset into the full
		// file — the parser only saw lines after fromLine, so both its
		// chunk text and its line numbers are relative to the delta.
		baseSeq := 0
		if fromLine > 0 {
			if existing == nil {
				idx.warnf("  WARN: append %s: document state is missing\n", f.Path)
				failed++
				continue
			}
			if ms, err := idx.db.MaxChunkSeqContext(idx.ctx(), existing.ID); err != nil {
				idx.warnf("  WARN: chunk seq %s: %v\n", f.Path, err)
				failed++
				continue
			} else {
				baseSeq = ms + 1
			}
		}
		nextSeq := baseSeq
		var indexChunks []store.IndexChunk
		text := batch.Text
		if text != "" {
			maxSize, _ := idx.chunkSize()
			chunks := chunk.ChunkConversation(text, maxSize)
			for i := range chunks {
				chunks[i].Seq = baseSeq + i
				if fromLine > 0 {
					chunks[i].StartLine += fromLine
					chunks[i].EndLine += fromLine
				}
				indexChunks = append(indexChunks, store.IndexChunk{
					Seq:       chunks[i].Seq,
					Content:   chunks[i].Content,
					StartLine: chunks[i].StartLine,
					EndLine:   chunks[i].EndLine,
				})
			}
			nextSeq = baseSeq + len(chunks)
		}

		for _, img := range batch.Images {
			indexChunks = append(indexChunks, store.IndexChunk{
				Seq:       nextSeq,
				Content:   img.Context,
				ChunkType: store.ChunkTypeImage,
				ImagePath: img.SavedPath,
			})
			nextSeq++
			totalImages++
		}

		request := store.DocumentIndex{
			CollectionID: col.ID,
			Path:         f.Path,
			Title:        title,
			Mtime:        f.Mtime,
			LineCount:    lineCount,
			FTSContent:   text,
			Chunks:       indexChunks,
		}
		// Tag only on the initial (replace) write: append passes must not
		// recompute tags for the whole session on every delta. The append
		// path in the store upserts fast fields without deleting them, so
		// the initial tags survive; they are refreshed on the next full
		// sync of the session.
		if fromLine == 0 && text != "" {
			request.FastFields = idx.semanticFastFields(idx.ctx(), f.Path, indexChunks)
		}
		if fromLine == 0 {
			_, err = idx.writer.UpsertAndReplaceIndex(idx.ctx(), request)
		} else {
			_, err = idx.writer.UpsertAndAppendIndex(idx.ctx(), request)
		}
		if err != nil {
			idx.warnf("  WARN: index %s: %v\n", f.Path, err)
			failed++
			continue
		}

		indexed++
	}

	idx.addReport(SyncReport{Indexed: indexed, Skipped: skipped, Failed: failed})
	idx.log.Printf("  Synced: %d indexed, %d unchanged", indexed, skipped)
	if totalImages > 0 {
		idx.log.Printf(", %d images", totalImages)
	}
	if failed > 0 {
		idx.log.Printf(", %d failed", failed)
	}
	idx.log.Printf("\n")
	return nil
}

// cleanupOrphans deletes every document of colID whose path is not in
// livePaths. A nil livePaths deletes all documents (full-reindex path).
// The returned error comes from ListDocumentPaths; callers that treat
// cleanup as best-effort may ignore it (removed is 0 then). label is used
// in the log line "Removed %d stale <label>"; an empty label disables logging.
func (idx *Indexer) cleanupOrphans(colID int64, livePaths map[string]bool, label string) (int, error) {
	removed, err := idx.db.DeleteOrphansContext(idx.ctx(), colID, livePaths)
	if err != nil {
		return 0, err
	}
	if removed > 0 && label != "" {
		idx.log.Printf("  Removed %d stale %s\n", removed, label)
	}
	return removed, nil
}

func toIndexChunks(chunks []chunk.Chunk, withLines bool) []store.IndexChunk {
	out := make([]store.IndexChunk, 0, len(chunks))
	for _, c := range chunks {
		indexed := store.IndexChunk{Seq: c.Seq, Content: c.Content}
		if withLines {
			indexed.StartLine = c.StartLine
			indexed.EndLine = c.EndLine
		}
		out = append(out, indexed)
	}
	return out
}

func (idx *Indexer) cleanupStaleCodeDocuments(colID int64, files []source.CodeFileInfo) error {
	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	_, err := idx.cleanupOrphans(colID, diskPaths, "documents")
	return err
}
