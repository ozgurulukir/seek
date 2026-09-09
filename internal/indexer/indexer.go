package indexer

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ozgurulukir/seek/internal/chunk"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/extractor"
	"github.com/ozgurulukir/seek/internal/extractor/builtin"
	"github.com/ozgurulukir/seek/internal/extractor/xberg"
	"github.com/ozgurulukir/seek/internal/source"
	"github.com/ozgurulukir/seek/internal/source/parserdef"
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
	if cfg.Config.OCR.Enabled && cfg.Config.OCR.APIKey != "" && !cfg.Config.OfflineOnly() {
		ocr = embed.NewOCRClient(cfg.Config.OCR.BaseURL, cfg.Config.OCR.APIKey, cfg.Config.OCR.Model)
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
	err := h(idx.ctx(), idx, col)
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

		lineCount, err := source.CountLines(f.Path)
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

func (idx *Indexer) syncClaude(col *store.Collection) error {
	return idx.syncConversation(col, source.ScanClaudeFiles,
		func(path string, fromLine int) (ConversationBatch, error) {
			convID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			msgs, imgs, err := source.ParseClaudeFileWithImages(path, fromLine, convID)
			batch := ConversationBatch{SessionID: convID, Images: imgs}
			if len(msgs) > 0 {
				batch.Text = source.ClaudeConversationToText(msgs)
				for _, msg := range msgs {
					if msg.Role == source.RoleUser {
						batch.Title = source.Truncate(msg.Content, source.TitleMaxLen)
						break
					}
				}
			}
			return batch, err
		},
		nil)
}

func (idx *Indexer) syncCodex(col *store.Collection) error {
	threadNames := source.LoadCodexThreadNames()
	return idx.syncConversation(col, source.ScanCodexFiles,
		func(path string, fromLine int) (ConversationBatch, error) {
			msgs, sessionID, imgs, err := source.ParseCodexFileWithImages(path, fromLine)
			batch := ConversationBatch{SessionID: sessionID, Images: imgs}
			if len(msgs) > 0 {
				batch.Text = source.ConversationToText(msgs)
				for _, msg := range msgs {
					if msg.Role == source.RoleUser {
						batch.Title = source.Truncate(msg.Content, source.TitleMaxLen)
						break
					}
				}
			}
			return batch, err
		},
		func(sessionID, defaultTitle string) string {
			if name, ok := threadNames[sessionID]; ok && name != "" {
				return name
			}
			return defaultTitle
		})
}

func (idx *Indexer) syncMarkdown(col *store.Collection) error {
	files, scanIssues, err := source.ScanMarkdown(col.Path, col.Pattern)
	if err != nil {
		return err
	}

	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	if len(scanIssues) == 0 {
		if _, err := idx.cleanupOrphans(col.ID, diskPaths, "documents"); err != nil {
			return fmt.Errorf("cleanup markdown documents: %w", err)
		}
	} else {
		for _, issue := range scanIssues {
			idx.warnf("  WARN: scan %s: %v\n", issue.Path, issue.Err)
			idx.recordFailure(issue.Path, "scan", issue.Err)
		}
	}

	var indexed, skipped, failed int
	failed += len(scanIssues)
	for _, f := range files {
		if err := idx.ctx().Err(); err != nil {
			return err
		}
		existing, err := idx.db.GetDocumentContext(idx.ctx(), col.ID, f.Path)
		if err == nil && existing.ContentHash == f.ContentHash {
			if existing.Mtime != f.Mtime {
				if err := idx.db.UpdateDocumentMtimeContext(idx.ctx(), existing.ID, f.Mtime); err != nil {
					return fmt.Errorf("update markdown mtime: %w", err)
				}
			}
			skipped++
			continue
		}

		maxSize, overlap := idx.chunkSize()
		_, err = idx.writer.UpsertAndReplaceIndex(idx.ctx(), store.DocumentIndex{
			CollectionID: col.ID,
			Path:         f.Path,
			Title:        f.Title,
			ContentHash:  f.ContentHash,
			Mtime:        f.Mtime,
			LineCount:    f.LineCount,
			FTSContent:   f.Content,
			Chunks:       toIndexChunks(chunk.ChunkMarkdown(f.Content, maxSize, overlap), true),
			FastFields:   f.Metadata,
		})
		if err != nil {
			idx.warnf("  WARN: index %s: %v\n", f.Path, err)
			failed++
			continue
		}
		indexed++
	}

	idx.addReport(SyncReport{Indexed: indexed, Skipped: skipped, Failed: failed})
	idx.log.Printf("  Synced: %d indexed, %d unchanged", indexed, skipped)
	if failed > 0 {
		idx.log.Printf(", %d failed", failed)
	}
	idx.log.Printf("\n")
	return nil
}

func (idx *Indexer) syncImage(col *store.Collection) error {
	files, scanIssues, err := source.ScanImages(col.Path)
	if err != nil {
		return err
	}

	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	if len(scanIssues) == 0 {
		if _, err := idx.cleanupOrphans(col.ID, diskPaths, "images"); err != nil {
			return fmt.Errorf("cleanup images: %w", err)
		}
	} else {
		for _, issue := range scanIssues {
			idx.warnf("  WARN: scan %s: %v\n", issue.Path, issue.Err)
			idx.recordFailure(issue.Path, "scan", issue.Err)
		}
	}

	var indexed, skipped, failed int
	failed += len(scanIssues)
	for _, f := range files {
		if err := idx.ctx().Err(); err != nil {
			return err
		}
		existing, err := idx.db.GetDocumentContext(idx.ctx(), col.ID, f.Path)
		if err == nil && existing.ContentHash == f.ContentHash {
			skipped++
			continue
		}

		_, err = idx.writer.UpsertAndReplaceIndex(idx.ctx(), store.DocumentIndex{
			CollectionID: col.ID,
			Path:         f.Path,
			Title:        f.Name,
			ContentHash:  f.ContentHash,
			Mtime:        f.Mtime,
			LineCount:    0,
			FTSContent:   f.Name,
			Chunks: []store.IndexChunk{{
				Seq:       0,
				Content:   f.Name,
				ChunkType: store.ChunkTypeImage,
				ImagePath: f.Path,
			}},
		})
		if err != nil {
			idx.warnf("  WARN: index %s: %v\n", f.Path, err)
			failed++
			continue
		}
		indexed++
	}

	idx.addReport(SyncReport{Indexed: indexed, Skipped: skipped, Failed: failed})
	idx.log.Printf("  Synced: %d indexed, %d unchanged", indexed, skipped)
	if failed > 0 {
		idx.log.Printf(", %d failed", failed)
	}
	idx.log.Printf("\n")
	return nil
}

func (idx *Indexer) syncPdf(col *store.Collection) error {
	files, scanIssues, err := source.ScanPdfs(col.Path)
	if err != nil {
		return err
	}

	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	if len(scanIssues) == 0 {
		if _, err := idx.cleanupOrphans(col.ID, diskPaths, "PDFs"); err != nil {
			return fmt.Errorf("cleanup PDFs: %w", err)
		}
	} else {
		for _, issue := range scanIssues {
			idx.warnf("  WARN: scan %s: %v\n", issue.Path, issue.Err)
			idx.recordFailure(issue.Path, "scan", issue.Err)
		}
	}

	ext, err := idx.extractorFor(col)
	if err != nil {
		return fmt.Errorf("extractor: %w", err)
	}

	var indexed, skipped, failed int
	failed += len(scanIssues)
	for _, f := range files {
		if err := idx.ctx().Err(); err != nil {
			return err
		}
		existing, err := idx.db.GetDocumentContext(idx.ctx(), col.ID, f.Path)
		if err == nil && existing.ContentHash == f.ContentHash {
			skipped++
			continue
		}

		// The builtin backend rasterizes pages (Result.Pages) and extracts
		// embedded/OCR text; xberg returns page text in Result.Content with no
		// page images. We handle both: store page images when present, else
		// fall back to chunking the extracted text.
		res, err := ext.Extract(idx.ctx(), f.Path)
		if err != nil {
			idx.warnf("  WARN: extract %s: %v\n", f.Path, err)
			failed++
			continue
		}

		pageCount := len(res.Pages)
		var pageText strings.Builder
		var indexChunks []store.IndexChunk
		if pageCount > 0 {
			// Page-oriented result (builtin PDF path): one image chunk per page.
			for _, pg := range res.Pages {
				var cb strings.Builder
				seqStr := strconv.Itoa(pg.Seq + 1)

				length := len("PDF page ") + len(seqStr) + len(" of ") + len(f.Name)
				if pg.Text != "" {
					length += 1 + len(pg.Text)
				}
				cb.Grow(length)

				cb.WriteString("PDF page ")
				cb.WriteString(seqStr)
				cb.WriteString(" of ")
				cb.WriteString(f.Name)

				if pg.Text != "" {
					cb.WriteByte('\n')
					cb.WriteString(pg.Text)
					pageText.WriteString(pg.Text)
					pageText.WriteString("\n")
				}
				content := cb.String()
				indexChunks = append(indexChunks, store.IndexChunk{
					Seq:       pg.Seq,
					Content:   content,
					ChunkType: store.ChunkTypeImage,
					ImagePath: pg.Path,
				})
			}
		} else if res.Content != "" {
			// Text-only result (e.g. xberg backend for PDF): chunk as markdown.
			maxSize, overlap := idx.chunkSize()
			indexChunks = toIndexChunks(chunk.ChunkMarkdown(res.Content, maxSize, overlap), false)
			pageText.WriteString(res.Content)
		}

		_, err = idx.writer.UpsertAndReplaceIndex(idx.ctx(), store.DocumentIndex{
			CollectionID: col.ID,
			Path:         f.Path,
			Title:        f.Name,
			ContentHash:  f.ContentHash,
			Mtime:        f.Mtime,
			LineCount:    pageCount,
			FTSContent:   pageText.String(),
			Chunks:       indexChunks,
		})
		if err != nil {
			idx.warnf("  WARN: index %s: %v\n", f.Path, err)
			failed++
			continue
		}

		indexed++
		if pageCount > 0 {
			idx.log.Printf("  Indexed %s (%d pages)\n", f.Name, pageCount)
		} else {
			idx.log.Printf("  Indexed %s\n", f.Name)
		}
	}

	idx.addReport(SyncReport{Indexed: indexed, Skipped: skipped, Failed: failed})
	idx.log.Printf("PDFs: %d indexed, %d skipped", indexed, skipped)
	if failed > 0 {
		idx.log.Printf(", %d failed", failed)
	}
	idx.log.Printf("\n")
	return nil
}

// syncDocuments indexes a universal documents collection. Unlike markdown/pdf/
// images, the source files are rich formats (docx/xlsx/pptx/epub/html/eml/...)
// that require an extraction backend to produce text. The active backend
// (builtin or xberg) is resolved from config; xberg is the typical choice here
// since the builtin backend only handles markdown/pdf/images.
//
// Flow mirrors syncMarkdown: scan → orphan cleanup → hash-skip → extract →
// upsert → FTS → markdown-chunk → insert. xberg returns markdown, which chunks
// well and preserves structure (headings, tables, lists).
func (idx *Indexer) syncDocuments(col *store.Collection) error {
	files, scanIssues, err := source.ScanDocuments(col.Path)
	if err != nil {
		return err
	}

	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	if len(scanIssues) == 0 {
		if _, err := idx.cleanupOrphans(col.ID, diskPaths, "documents"); err != nil {
			return fmt.Errorf("cleanup documents: %w", err)
		}
	} else {
		for _, issue := range scanIssues {
			idx.warnf("  WARN: scan %s: %v\n", issue.Path, issue.Err)
			idx.recordFailure(issue.Path, "scan", issue.Err)
		}
	}

	ext, err := idx.extractorFor(col)
	if err != nil {
		return fmt.Errorf("extractor: %w", err)
	}

	var indexed, skipped, failed, unsupported int
	failed += len(scanIssues)
	for _, f := range files {
		status := idx.syncDocumentFile(col, f, ext)
		switch status {
		case docStatusSkipped:
			skipped++
		case docStatusUnsupported:
			unsupported++
		case docStatusFailed:
			failed++
		case docStatusIndexed:
			indexed++
		}
	}

	idx.addReport(SyncReport{Indexed: indexed, Skipped: skipped, Unsupported: unsupported, Failed: failed})
	idx.log.Printf("Documents: %d indexed, %d unchanged", indexed, skipped)
	if unsupported > 0 {
		idx.log.Printf(", %d unsupported", unsupported)
	}
	if failed > 0 {
		idx.log.Printf(", %d failed", failed)
	}
	idx.log.Printf("\n")
	return nil
}

// syncCode indexes a source code collection. It scans for supported programming
// languages, ignores binaries/vendors/lockfiles/.gitignore entries, extracts
// relative path titles, chunks code structurally, and writes fastfield metadata.
func (idx *Indexer) syncCode(col *store.Collection) error {
	files, skippedPaths, err := source.ScanCodeWithWarnings(col.Path, col.Pattern)
	if err != nil {
		return err
	}
	for _, p := range skippedPaths {
		idx.warnf("  WARN: scan skipped %s (unreadable)\n", p)
		idx.recordFailure(p, "scan", fmt.Errorf("unreadable path"))
	}

	if len(skippedPaths) == 0 {
		if err := idx.cleanupStaleCodeDocuments(col.ID, files); err != nil {
			return fmt.Errorf("cleanup code documents: %w", err)
		}
	} else {
		idx.warnf("  WARN: skipping code orphan cleanup due to %d scan issue(s)\n", len(skippedPaths))
	}

	var indexed, skipped, failed int
	failed += len(skippedPaths)
	for _, f := range files {
		if err := idx.ctx().Err(); err != nil {
			return err
		}
		isSkipped, err := idx.indexCodeFile(col, f)
		if err != nil {
			idx.warnf("  WARN: %v\n", err)
			failed++
			continue
		}
		if isSkipped {
			skipped++
		} else {
			indexed++
		}
	}

	idx.addReport(SyncReport{Indexed: indexed, Skipped: skipped, Failed: failed})
	idx.log.Printf("Code: %d indexed, %d unchanged", indexed, skipped)
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

func (idx *Indexer) indexCodeFile(col *store.Collection, f source.CodeFileInfo) (bool, error) {
	existing, err := idx.db.GetDocumentContext(idx.ctx(), col.ID, f.Path)
	if err == nil && existing.ContentHash == f.ContentHash {
		if existing.Mtime != f.Mtime {
			if err := idx.db.UpdateDocumentMtimeContext(idx.ctx(), existing.ID, f.Mtime); err != nil {
				return false, fmt.Errorf("update code mtime: %w", err)
			}
		}
		return true, nil
	}

	maxSize, overlap := idx.chunkSize()
	_, err = idx.writer.UpsertAndReplaceIndex(idx.ctx(), store.DocumentIndex{
		CollectionID: col.ID,
		Path:         f.Path,
		Title:        f.Title,
		ContentHash:  f.ContentHash,
		Mtime:        f.Mtime,
		LineCount:    f.LineCount,
		FTSContent:   f.Content,
		Chunks:       toIndexChunks(chunk.ChunkCode(f.Content, f.Language, maxSize, overlap), true),
		FastFields: map[string]string{
			"lang":     f.Language,
			"ext":      f.Extension,
			"filename": filepath.Base(f.Path),
			"rel_path": f.RelativePath,
			"repo":     col.Name,
		},
	})
	if err != nil {
		return false, fmt.Errorf("index %s: %w", f.Path, err)
	}

	return false, nil
}

// syncParserDef indexes a schema-driven parser collection (§6.6).
// Flow: load → match → version check (reindex vs incremental) → SyncSessions →
// document upsert → FTS + chunks → metadata (FastFields) → orphan cleanup.
func (idx *Indexer) syncParserDef(col *store.Collection) error {
	// 1. Load the parser schema (embedded default + user override).
	def, err := parserdef.Load(col.ParserName)
	if err != nil {
		return fmt.Errorf("load parser %q: %w", col.ParserName, err)
	}

	// 2. Match: detect source + version from the environment.
	src, ver, files, err := def.Match()
	if err != nil {
		return fmt.Errorf("detect source for %q: %w", col.ParserName, err)
	}

	// 3. Version check: if the schema version changed, do a full reindex.
	reindex := col.ParserVersion != ver.Version
	var since time.Time
	if reindex {
		idx.log.Printf("  Parser %q version changed: %d → %d (full reindex)\n",
			col.ParserName, col.ParserVersion, ver.Version)
		if err := idx.reindexParserCollection(col); err != nil {
			return fmt.Errorf("reindex: %w", err)
		}
		if err := idx.db.UpdateCollectionParserVersionContext(idx.ctx(), col.ID, ver.Version); err != nil {
			return fmt.Errorf("update parser version: %w", err)
		}
		col.ParserVersion = ver.Version
		// since stays zero → full fetch.
	} else {
		// Incremental: only sessions with cursor > max(existing mtime).
		// We store cursors as milliseconds (see write path below) so sub-second
		// precision is preserved — critical for epoch_ms cursors (opencode).
		maxMtime, err := idx.db.MaxDocumentMtimeContext(idx.ctx(), col.ID)
		if err != nil {
			return fmt.Errorf("read parser cursor: %w", err)
		}
		if maxMtime > 0 {
			since = time.UnixMilli(int64(maxMtime))
		}
	}

	// 4. Fetch sessions.
	sessions, sErrs, err := parserdef.SyncSessions(src, ver, files, since)
	if err != nil {
		return fmt.Errorf("sync sessions: %w", err)
	}
	for _, se := range sErrs {
		idx.warnf("  WARN: session %s: %v\n", se.SessionID, se.Err)
		idx.recordFailure(se.SessionID, "scan", se.Err)
	}

	// 5. Index each session.
	var indexed, skipped, failed int
	seenPaths := make(map[string]bool)
	for _, sess := range sessions {
		if err := idx.ctx().Err(); err != nil {
			return err
		}
		docPath := sess.SrcPath + "#" + sess.ID
		seenPaths[docPath] = true

		// Messages == nil means "unchanged" (incremental sync skip) — the
		// session still exists in the source, so track it for orphan detection.
		if sess.Messages == nil {
			skipped++
			continue
		}

		// Messages present but empty — skip indexing (no content).
		if len(sess.Messages) == 0 {
			skipped++
			continue
		}

		// Title: schema title → first user message fallback.
		title := sess.Title
		if title == "" {
			for _, m := range sess.Messages {
				if m.Role == source.RoleUser {
					title = source.Truncate(m.Content, source.TitleMaxLen)
					break
				}
			}
		}
		if title == "" {
			title = sess.ID
		}

		// Convert messages to text (same format as native claude/codex).
		text := parserMessagesToText(sess.Messages)
		cursorUnix := 0.0
		if !sess.Cursor.IsZero() {
			cursorUnix = float64(sess.Cursor.UnixMilli())
		}

		// FTS + chunks: full rewrite (sessions are non-append-only).
		maxSize, _ := idx.chunkSize()
		_, err = idx.writer.UpsertAndReplaceIndex(idx.ctx(), store.DocumentIndex{
			CollectionID: col.ID,
			Path:         docPath,
			Title:        title,
			Mtime:        cursorUnix,
			LineCount:    len(sess.Messages),
			FTSContent:   text,
			Chunks:       toIndexChunks(chunk.ChunkConversation(text, maxSize), false),
			FastFields:   sess.Metadata,
		})
		if err != nil {
			idx.warnf("  WARN: index %s: %v\n", docPath, err)
			failed++
			continue
		}

		indexed++
	}

	// 6. Orphan cleanup: remove documents for sessions no longer in the source.
	// Skip cleanup if any source DB failed to scan — a transient error (busy
	// timeout, locked DB) would otherwise cause us to delete sessions that are
	// still present but couldn't be read this cycle.
	if len(sErrs) == 0 {
		if _, err := idx.cleanupOrphans(col.ID, seenPaths, "sessions"); err != nil {
			return fmt.Errorf("cleanup sessions: %w", err)
		}
	} else {
		idx.warnf("  WARN: skipping orphan cleanup due to %d scan error(s)\n", len(sErrs))
	}

	idx.addReport(SyncReport{Indexed: indexed, Skipped: skipped, Failed: failed + len(sErrs)})
	idx.log.Printf("  Synced: %d indexed, %d unchanged", indexed, skipped)
	if failed > 0 || len(sErrs) > 0 {
		idx.log.Printf(", %d failed", failed+len(sErrs))
	}
	idx.log.Printf("\n")
	return nil
}

// reindexParserCollection deletes all documents/chunks/FTS for a parser collection
// so the next sync is a full re-fetch (§6.6).
func (idx *Indexer) reindexParserCollection(col *store.Collection) error {
	// nil livePaths → every existing document is stale; empty label → no log.
	_, err := idx.cleanupOrphans(col.ID, nil, "")
	return err
}

// parserMessagesToText converts parserdef messages to the [role]: content format
// used by the native claude/codex parsers (ClaudeConversationToText, codex.go:364).
func parserMessagesToText(messages []parserdef.Message) string {
	var b strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&b, "[%s]: %s\n\n", m.Role, m.Content)
	}
	return b.String()
}

type docSyncStatus int

const (
	docStatusIndexed docSyncStatus = iota
	docStatusSkipped
	docStatusUnsupported
	docStatusFailed
)

func (idx *Indexer) syncDocumentFile(col *store.Collection, f source.DocumentFile, ext extractor.Extractor) docSyncStatus {
	existing, err := idx.db.GetDocumentContext(idx.ctx(), col.ID, f.Path)
	if err == nil && existing.ContentHash == f.ContentHash {
		if existing.Mtime != f.Mtime {
			if err := idx.db.UpdateDocumentMtimeContext(idx.ctx(), existing.ID, f.Mtime); err != nil {
				idx.recordFailure(f.Path, "persistence", err)
				return docStatusFailed
			}
		}
		return docStatusSkipped
	}

	// Skip files the active backend doesn't handle (e.g. a format in the
	// scanner set that xberg's /formats doesn't list). Counted separately
	// from failures so the summary distinguishes "unsupported" from "errored".
	if !ext.Supports(f.Path) {
		idx.report.Errors = append(idx.report.Errors, SyncFailure{Path: f.Path, Kind: "unsupported", Error: "extractor does not support file"})
		return docStatusUnsupported
	}

	res, err := ext.Extract(idx.ctx(), f.Path)
	if err != nil {
		idx.warnf("  WARN: extract %s: %v\n", f.Path, err)
		idx.recordFailure(f.Path, "extraction", err)
		return docStatusFailed
	}

	lineCount := strings.Count(res.Content, "\n") + 1
	maxSize, overlap := idx.chunkSize()
	if _, err := idx.writer.UpsertAndReplaceIndex(idx.ctx(), store.DocumentIndex{
		CollectionID: col.ID,
		Path:         f.Path,
		Title:        res.Title,
		ContentHash:  f.ContentHash,
		Mtime:        f.Mtime,
		LineCount:    lineCount,
		FTSContent:   res.Content,
		Chunks:       toIndexChunks(chunk.ChunkMarkdown(res.Content, maxSize, overlap), true),
	}); err != nil {
		idx.warnf("  WARN: index %s: %v\n", f.Path, err)
		idx.recordFailure(f.Path, "persistence", err)
		return docStatusFailed
	}

	return docStatusIndexed
}
