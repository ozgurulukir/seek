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
	cfg *config.AppConfig
	db  *store.Store
	log Logger
	// ext is an explicit override for the extraction backend, taking precedence
	// over both per-collection backend and the config default. Set via
	// WithExtractor (e.g. from a --backend flag). When nil, the backend is
	// resolved per collection (see extractorFor).
	ext extractor.Extractor
	// extCache memoizes extractors by backend name so repeated syncs of
	// collections with the same backend don't rebuild (and, for xberg,
	// re-probe health) on every collection.
	extCache map[string]extractor.Extractor
}

func New(cfg *config.AppConfig, db *store.Store) *Indexer {
	return &Indexer{
		cfg:      cfg,
		db:       db,
		log:      defaultLogger{},
		extCache: make(map[string]extractor.Extractor),
	}
}

// WithExtractor overrides the extraction backend for all collections (e.g. from
// a --backend flag). Pass nil to revert to per-collection / config resolution.
func (idx *Indexer) WithExtractor(ext extractor.Extractor) *Indexer {
	idx.ext = ext
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
// Empty backend strings fall through to the config default. Extractors are
// memoized by backend name so the xberg health probe runs at most once.
func (idx *Indexer) extractorFor(col *store.Collection) (extractor.Extractor, error) {
	if idx.ext != nil {
		return idx.ext, nil
	}
	backend := col.Backend
	if backend == "" {
		backend = idx.cfg.Config.Extractor.Backend
	}
	if cached, ok := idx.extCache[backend]; ok {
		return cached, nil
	}
	ext, err := NewExtractor(idx.cfg, backend)
	if err != nil {
		return nil, err
	}
	idx.extCache[backend] = ext
	return ext, nil
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
	if cfg.Config.OCR.Enabled && cfg.Config.OCR.APIKey != "" {
		ocr = embed.NewOCRClient(cfg.Config.OCR.BaseURL, cfg.Config.OCR.APIKey, cfg.Config.OCR.Model)
	}
	switch backend {
	case "", "builtin":
		return builtin.New(ocr, cfg.CacheDir), nil
	case "xberg":
		return xberg.NewWithHealthCheck(cfg.Config.Extractor, cfg.CacheDir)
	default:
		return nil, fmt.Errorf("unknown extractor backend %q (want builtin or xberg)", backend)
	}
}

// ctx returns the context for extraction calls. Today this is Background; the
// indirection leaves room to plumb a command-level cancellation context later
// without touching every call site.
func (idx *Indexer) ctx() context.Context { return context.Background() }

func (idx *Indexer) WithLogger(l Logger) *Indexer {
	idx.log = l
	return idx
}

func (idx *Indexer) SyncCollection(col *store.Collection) error {
	switch col.Type {
	case "markdown":
		return idx.syncMarkdown(col)
	case "claude":
		return idx.syncClaude(col)
	case "codex":
		return idx.syncCodex(col)
	case "images":
		return idx.syncImage(col)
	case "pdf":
		return idx.syncPdf(col)
	case "documents":
		return idx.syncDocuments(col)
	case "code":
		return idx.syncCode(col)
	case "parser":
		return idx.syncParserDef(col)
	default:
		return fmt.Errorf("unknown collection type: %s", col.Type)
	}
}

// syncConversation abstracts the heavily duplicated logic between Claude and Codex
func (idx *Indexer) syncConversation(
	col *store.Collection,
	scanFiles func() ([]source.ConversationFile, error),
	parseFile func(path string, fromLine int) (map[string]interface{}, string, []source.ConversationImage, error),
	toText func(map[string]interface{}) string,
	extractTitle func(map[string]interface{}) string,
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
	idx.cleanupOrphans(col.ID, diskPaths, "conversations")

	var indexed, skipped, totalImages, failed int

	for _, f := range files {
		existing, err := idx.db.GetDocument(col.ID, f.Path)
		if err == nil && existing.Mtime >= f.Mtime {
			skipped++
			continue
		}

		lineCount, err := source.CountLines(f.Path)
		if err != nil {
			idx.log.Printf("  WARN: count lines %s: %v\n", f.Path, err)
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

		messages, sessionID, images, err := parseFile(f.Path, fromLine)
		if err != nil {
			idx.log.Printf("  WARN: parse %s: %v\n", f.Path, err)
			failed++
			continue
		}

		if messages == nil && len(images) == 0 {
			if existing != nil {
				if fromLine == 0 {
					// A full re-parse that yields nothing means the file no
					// longer contains parseable content (e.g. truncated to
					// empty or to metadata-only lines). Remove the stale
					// document so its FTS entry and chunks go with it,
					// mirroring deleted files.
					idx.db.DeleteDocument(existing.ID)
				} else {
					// Append that produced no new content: just record the
					// mtime so subsequent syncs skip this file without
					// re-parsing it.
					idx.db.UpdateDocumentMtime(existing.ID, f.Mtime)
				}
			}
			skipped++
			continue
		}

		title := filepath.Base(f.Path)
		if fromLine == 0 && extractTitle != nil {
			if t := extractTitle(messages); t != "" {
				title = t
			}
		}
		if getTitle != nil {
			title = getTitle(sessionID, title)
		}

		docID, err := idx.db.UpsertDocument(col.ID, f.Path, title, "", f.Mtime, lineCount)
		if err != nil {
			idx.log.Printf("  WARN: upsert %s: %v\n", f.Path, err)
			failed++
			continue
		}

		// On an append, new chunks must continue after the seqs from the
		// previous sync and their line spans must be offset into the full
		// file — the parser only saw lines after fromLine, so both its
		// chunk text and its line numbers are relative to the delta.
		baseSeq := 0
		if fromLine > 0 {
			if ms, err := idx.db.MaxChunkSeq(docID); err != nil {
				idx.log.Printf("  WARN: chunk seq %s: %v\n", f.Path, err)
			} else {
				baseSeq = ms + 1
			}
		}
		nextSeq := baseSeq
		if messages != nil {
			text := toText(messages)
			if fromLine > 0 {
				if err := idx.db.AppendFTS(docID, text); err != nil {
					idx.log.Printf("  WARN: fts %s: %v\n", f.Path, err)
				}
			} else {
				if err := idx.db.UpsertFTS(docID, title, text); err != nil {
					idx.log.Printf("  WARN: fts %s: %v\n", f.Path, err)
				}
				idx.db.DeleteChunksForDocument(docID)
			}

			maxSize, _ := idx.chunkSize()
			chunks := chunk.ChunkConversation(text, maxSize)
			for i := range chunks {
				chunks[i].Seq = baseSeq + i
				if fromLine > 0 {
					chunks[i].StartLine += fromLine
					chunks[i].EndLine += fromLine
				}
				if err := idx.db.InsertChunkWithLines(docID, chunks[i].Seq, chunks[i].Content, chunks[i].StartLine, chunks[i].EndLine, nil); err != nil {
					idx.log.Printf("  WARN: embed %s: %v\n", f.Path, err)
				}
			}
			nextSeq = baseSeq + len(chunks)
		}

		for _, img := range images {
			if err := idx.db.InsertImageChunk(docID, nextSeq, img.Context, img.SavedPath, nil); err != nil {
				idx.log.Printf("  WARN: image chunk %s: %v\n", img.SavedPath, err)
				continue
			}
			nextSeq++
			totalImages++
		}

		indexed++
	}

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
		func(path string, fromLine int) (map[string]interface{}, string, []source.ConversationImage, error) {
			convID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			msgs, imgs, err := source.ParseClaudeFileWithImages(path, fromLine, convID)
			if len(msgs) == 0 {
				return nil, convID, imgs, err
			}
			return map[string]interface{}{"msgs": msgs}, convID, imgs, err
		},
		func(m map[string]interface{}) string {
			msgs, _ := m["msgs"].([]source.ClaudeMessage)
			return source.ClaudeConversationToText(msgs)
		},
		func(m map[string]interface{}) string {
			msgs, _ := m["msgs"].([]source.ClaudeMessage)
			for _, msg := range msgs {
				if msg.Role == source.RoleUser {
					return source.Truncate(msg.Content, source.TitleMaxLen)
				}
			}
			return ""
		},
		nil)
}

func (idx *Indexer) syncCodex(col *store.Collection) error {
	threadNames := source.LoadCodexThreadNames()
	return idx.syncConversation(col, source.ScanCodexFiles,
		func(path string, fromLine int) (map[string]interface{}, string, []source.ConversationImage, error) {
			msgs, sessionID, imgs, err := source.ParseCodexFileWithImages(path, fromLine)
			if len(msgs) == 0 {
				return nil, sessionID, imgs, err
			}
			return map[string]interface{}{"msgs": msgs}, sessionID, imgs, err
		},
		func(m map[string]interface{}) string {
			msgs, _ := m["msgs"].([]source.CodexMessage)
			return source.ConversationToText(msgs)
		},
		func(m map[string]interface{}) string {
			msgs, _ := m["msgs"].([]source.CodexMessage)
			for _, msg := range msgs {
				if msg.Role == source.RoleUser {
					return source.Truncate(msg.Content, source.TitleMaxLen)
				}
			}
			return ""
		},
		func(sessionID, defaultTitle string) string {
			if name, ok := threadNames[sessionID]; ok && name != "" {
				return name
			}
			return defaultTitle
		})
}

func (idx *Indexer) syncMarkdown(col *store.Collection) error {
	files, err := source.ScanMarkdown(col.Path, col.Pattern)
	if err != nil {
		return err
	}

	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	idx.cleanupOrphans(col.ID, diskPaths, "documents")

	var indexed, skipped, failed int
	for _, f := range files {
		existing, err := idx.db.GetDocument(col.ID, f.Path)
		if err == nil && existing.ContentHash == f.ContentHash {
			if existing.Mtime != f.Mtime {
				idx.db.UpdateDocumentMtime(existing.ID, f.Mtime)
			}
			skipped++
			continue
		}

		docID, err := idx.db.UpsertDocument(col.ID, f.Path, f.Title, f.ContentHash, f.Mtime, f.LineCount)
		if err != nil {
			idx.log.Printf("  WARN: upsert %s: %v\n", f.Path, err)
			failed++
			continue
		}

		maxSize, overlap := idx.chunkSize()
		idx.replaceIndexText(docID, f.Path, f.Title, f.Content, chunk.ChunkMarkdown(f.Content, maxSize, overlap), true)
		indexed++
	}

	idx.log.Printf("  Synced: %d indexed, %d unchanged", indexed, skipped)
	if failed > 0 {
		idx.log.Printf(", %d failed", failed)
	}
	idx.log.Printf("\n")
	return nil
}

func (idx *Indexer) syncImage(col *store.Collection) error {
	files, err := source.ScanImages(col.Path)
	if err != nil {
		return err
	}

	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	idx.cleanupOrphans(col.ID, diskPaths, "images")

	var indexed, skipped, failed int
	for _, f := range files {
		existing, err := idx.db.GetDocument(col.ID, f.Path)
		if err == nil && existing.ContentHash == f.ContentHash {
			skipped++
			continue
		}

		docID, err := idx.db.UpsertDocument(col.ID, f.Path, f.Name, f.ContentHash, f.Mtime, 0)
		if err != nil {
			idx.log.Printf("  WARN: upsert %s: %v\n", f.Path, err)
			failed++
			continue
		}

		idx.db.DeleteChunksForDocument(docID)
		idx.db.InsertImageChunk(docID, 0, f.Name, f.Path, nil)
		indexed++
	}

	idx.log.Printf("  Synced: %d indexed, %d unchanged", indexed, skipped)
	if failed > 0 {
		idx.log.Printf(", %d failed", failed)
	}
	idx.log.Printf("\n")
	return nil
}

func (idx *Indexer) syncPdf(col *store.Collection) error {
	files, err := source.ScanPdfs(col.Path)
	if err != nil {
		return err
	}

	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	idx.cleanupOrphans(col.ID, diskPaths, "PDFs")

	ext, err := idx.extractorFor(col)
	if err != nil {
		return fmt.Errorf("extractor: %w", err)
	}

	var indexed, skipped, failed int
	for _, f := range files {
		existing, err := idx.db.GetDocument(col.ID, f.Path)
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
			idx.log.Printf("  WARN: extract %s: %v\n", f.Path, err)
			failed++
			continue
		}

		pageCount := len(res.Pages)
		docID, err := idx.db.UpsertDocument(col.ID, f.Path, f.Name, f.ContentHash, f.Mtime, pageCount)
		if err != nil {
			idx.log.Printf("  WARN: upsert %s: %v\n", f.Path, err)
			failed++
			continue
		}

		idx.db.DeleteChunksForDocument(docID)

		if pageCount > 0 {
			// Page-oriented result (builtin PDF path): one image chunk per page.
			var pageText strings.Builder
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
				idx.db.InsertImageChunk(docID, pg.Seq, content, pg.Path, nil)
			}
			if pageText.Len() > 0 {
				idx.db.UpsertFTS(docID, f.Name, pageText.String())
			}
		} else if res.Content != "" {
			// Text-only result (e.g. xberg backend for PDF): chunk as markdown.
			maxSize, overlap := idx.chunkSize()
			idx.replaceIndexText(docID, f.Path, f.Name, res.Content, chunk.ChunkMarkdown(res.Content, maxSize, overlap), false)
		}

		indexed++
		if pageCount > 0 {
			idx.log.Printf("  Indexed %s (%d pages)\n", f.Name, pageCount)
		} else {
			idx.log.Printf("  Indexed %s\n", f.Name)
		}
	}

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
	files, err := source.ScanDocuments(col.Path)
	if err != nil {
		return err
	}

	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	idx.cleanupOrphans(col.ID, diskPaths, "documents")

	ext, err := idx.extractorFor(col)
	if err != nil {
		return fmt.Errorf("extractor: %w", err)
	}

	var indexed, skipped, failed, unsupported int
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
	files, err := source.ScanCode(col.Path, col.Pattern)
	if err != nil {
		return err
	}

	idx.cleanupStaleCodeDocuments(col.ID, files)

	var indexed, skipped, failed int
	for _, f := range files {
		isSkipped, err := idx.indexCodeFile(col.ID, f)
		if err != nil {
			idx.log.Printf("  WARN: %v\n", err)
			failed++
			continue
		}
		if isSkipped {
			skipped++
		} else {
			indexed++
		}
	}

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
	existingPaths, err := idx.db.ListDocumentPaths(colID)
	if err != nil {
		return 0, err
	}
	removed := 0
	for path, docID := range existingPaths {
		if livePaths[path] {
			continue
		}
		idx.db.DeleteDocument(docID)
		removed++
	}
	if removed > 0 && label != "" {
		idx.log.Printf("  Removed %d stale %s\n", removed, label)
	}
	return removed, nil
}

// replaceIndexText rewrites the searchable content of a document: upserts the
// FTS entry, deletes old chunks, and inserts the given chunks (with line spans
// when withLines is true). FTS and chunk errors are logged, never fatal.
// label identifies the source in WARN lines (usually the file path).
func (idx *Indexer) replaceIndexText(docID int64, label, title, text string, chunks []chunk.Chunk, withLines bool) {
	if err := idx.db.UpsertFTS(docID, title, text); err != nil {
		idx.log.Printf("  WARN: fts %s: %v\n", label, err)
	}
	idx.db.DeleteChunksForDocument(docID)
	for _, c := range chunks {
		var err error
		if withLines {
			err = idx.db.InsertChunkWithLines(docID, c.Seq, c.Content, c.StartLine, c.EndLine, nil)
		} else {
			err = idx.db.InsertChunk(docID, c.Seq, c.Content, nil)
		}
		if err != nil {
			idx.log.Printf("  WARN: chunk %s: %v\n", label, err)
		}
	}
}

func (idx *Indexer) cleanupStaleCodeDocuments(colID int64, files []source.CodeFileInfo) {
	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	idx.cleanupOrphans(colID, diskPaths, "documents")
}

func (idx *Indexer) indexCodeFile(colID int64, f source.CodeFileInfo) (bool, error) {
	existing, err := idx.db.GetDocument(colID, f.Path)
	if err == nil && existing.ContentHash == f.ContentHash {
		if existing.Mtime != f.Mtime {
			idx.db.UpdateDocumentMtime(existing.ID, f.Mtime)
		}
		return true, nil
	}

	docID, err := idx.db.UpsertDocument(colID, f.Path, f.Title, f.ContentHash, f.Mtime, f.LineCount)
	if err != nil {
		return false, fmt.Errorf("upsert %s: %w", f.Path, err)
	}

	maxSize, overlap := idx.chunkSize()
	idx.replaceIndexText(docID, f.Path, f.Title, f.Content, chunk.ChunkCode(f.Content, f.Language, maxSize, overlap), true)

	// Fast field metadata
	_ = idx.db.FastFields().Set(docID, "lang", f.Language)
	_ = idx.db.FastFields().Set(docID, "ext", f.Extension)
	_ = idx.db.FastFields().Set(docID, "filename", filepath.Base(f.Path))
	_ = idx.db.FastFields().Set(docID, "rel_path", f.RelativePath)

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
		if err := idx.db.UpdateCollectionParserVersion(col.ID, ver.Version); err != nil {
			return fmt.Errorf("update parser version: %w", err)
		}
		col.ParserVersion = ver.Version
		// since stays zero → full fetch.
	} else {
		// Incremental: only sessions with cursor > max(existing mtime).
		// We store cursors as milliseconds (see write path below) so sub-second
		// precision is preserved — critical for epoch_ms cursors (opencode).
		maxMtime, _ := idx.db.MaxDocumentMtime(col.ID)
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
		idx.log.Printf("  WARN: session %s: %v\n", se.SessionID, se.Err)
	}

	// 5. Index each session.
	var indexed, skipped, failed int
	seenPaths := make(map[string]bool)
	for _, sess := range sessions {
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

		docID, err := idx.db.UpsertDocument(col.ID, docPath, title, "", cursorUnix, len(sess.Messages))
		if err != nil {
			idx.log.Printf("  WARN: upsert %s: %v\n", docPath, err)
			failed++
			continue
		}

		// FTS + chunks: full rewrite (sessions are non-append-only).
		maxSize, _ := idx.chunkSize()
		idx.replaceIndexText(docID, docPath, title, text, chunk.ChunkConversation(text, maxSize), false)

		// Metadata enrichment (§6.13): write known fields to fast_fields.
		for field, value := range sess.Metadata {
			if value == "" {
				continue
			}
			if err := idx.db.FastFields().Set(docID, field, value); err != nil {
				idx.log.Printf("  WARN: metadata %s=%s: %v\n", field, value, err)
			}
		}

		indexed++
	}

	// 6. Orphan cleanup: remove documents for sessions no longer in the source.
	// Skip cleanup if any source DB failed to scan — a transient error (busy
	// timeout, locked DB) would otherwise cause us to delete sessions that are
	// still present but couldn't be read this cycle.
	if len(sErrs) == 0 {
		idx.cleanupOrphans(col.ID, seenPaths, "sessions")
	} else {
		idx.log.Printf("  Skipping orphan cleanup due to %d scan error(s)\n", len(sErrs))
	}

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
	existing, err := idx.db.GetDocument(col.ID, f.Path)
	if err == nil && existing.ContentHash == f.ContentHash {
		if existing.Mtime != f.Mtime {
			idx.db.UpdateDocumentMtime(existing.ID, f.Mtime)
		}
		return docStatusSkipped
	}

	// Skip files the active backend doesn't handle (e.g. a format in the
	// scanner set that xberg's /formats doesn't list). Counted separately
	// from failures so the summary distinguishes "unsupported" from "errored".
	if !ext.Supports(f.Path) {
		return docStatusUnsupported
	}

	res, err := ext.Extract(idx.ctx(), f.Path)
	if err != nil {
		idx.log.Printf("  WARN: extract %s: %v\n", f.Path, err)
		return docStatusFailed
	}

	lineCount := strings.Count(res.Content, "\n") + 1
	docID, err := idx.db.UpsertDocument(col.ID, f.Path, res.Title, f.ContentHash, f.Mtime, lineCount)
	if err != nil {
		idx.log.Printf("  WARN: upsert %s: %v\n", f.Path, err)
		return docStatusFailed
	}

	maxSize, overlap := idx.chunkSize()
	idx.replaceIndexText(docID, f.Path, res.Title, res.Content, chunk.ChunkMarkdown(res.Content, maxSize, overlap), true)

	return docStatusIndexed
}
