package indexer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anthropics/seek/internal/chunk"
	"github.com/anthropics/seek/internal/config"
	"github.com/anthropics/seek/internal/embed"
	"github.com/anthropics/seek/internal/source"
	"github.com/anthropics/seek/internal/store"
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
}

func New(cfg *config.AppConfig, db *store.Store) *Indexer {
	return &Indexer{
		cfg: cfg,
		db:  db,
		log: defaultLogger{},
	}
}

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

	var indexed, skipped, totalImages int

	for _, f := range files {
		existing, err := idx.db.GetDocument(col.ID, f.Path)
		if err == nil && existing.Mtime >= f.Mtime {
			skipped++
			continue
		}

		lineCount, _ := source.CountLines(f.Path)
		if existing != nil && existing.LineCount >= lineCount {
			idx.db.UpdateDocumentMtime(existing.ID, f.Mtime)
			skipped++
			continue
		}

		fromLine := 0
		if existing != nil {
			fromLine = existing.LineCount
		}

		messages, sessionID, images, err := parseFile(f.Path, fromLine)
		if err != nil {
			continue
		}

		if messages == nil && len(images) == 0 {
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
			continue
		}

		nextSeq := 0
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

			chunks := chunk.ChunkConversation(text, 0)
			for _, c := range chunks {
				if err := idx.db.InsertChunk(docID, c.Seq, c.Content, nil); err != nil {
					idx.log.Printf("  WARN: embed %s: %v\n", f.Path, err)
				}
			}
			nextSeq = len(chunks)
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
	idx.log.Printf("\n")
	return nil
}

func (idx *Indexer) syncClaude(col *store.Collection) error {
	return idx.syncConversation(col, source.ScanClaudeFiles,
		func(path string, fromLine int) (map[string]interface{}, string, []source.ConversationImage, error) {
			convID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			msgs, imgs, err := source.ParseClaudeFileWithImages(path, fromLine, convID)
			return map[string]interface{}{"msgs": msgs}, convID, imgs, err
		},
		func(m map[string]interface{}) string {
			return source.ClaudeConversationToText(m["msgs"].([]source.ClaudeMessage))
		},
		func(m map[string]interface{}) string {
			for _, msg := range m["msgs"].([]source.ClaudeMessage) {
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
			return map[string]interface{}{"msgs": msgs}, sessionID, imgs, err
		},
		func(m map[string]interface{}) string {
			return source.ConversationToText(m["msgs"].([]source.CodexMessage))
		},
		func(m map[string]interface{}) string {
			for _, msg := range m["msgs"].([]source.CodexMessage) {
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

	if existingPaths, err := idx.db.ListDocumentPaths(col.ID); err == nil {
		removed := 0
		for path, docID := range existingPaths {
			if !diskPaths[path] {
				idx.db.DeleteDocument(docID)
				removed++
			}
		}
		if removed > 0 {
			idx.log.Printf("  Removed %d stale documents\n", removed)
		}
	}

	var indexed, skipped int
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
			continue
		}

		if err := idx.db.UpsertFTS(docID, f.Title, f.Content); err != nil {
			idx.log.Printf("  WARN: fts %s: %v\n", f.Path, err)
		}

		idx.db.DeleteChunksForDocument(docID)
		chunks := chunk.ChunkMarkdown(f.Content, 0, 0)
		for _, c := range chunks {
			idx.db.InsertChunk(docID, c.Seq, c.Content, nil)
		}
		indexed++
	}

	idx.log.Printf("  Synced: %d indexed, %d unchanged\n", indexed, skipped)
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
	if existingPaths, err := idx.db.ListDocumentPaths(col.ID); err == nil {
		removed := 0
		for path, docID := range existingPaths {
			if !diskPaths[path] {
				idx.db.DeleteDocument(docID)
				removed++
			}
		}
		if removed > 0 {
			idx.log.Printf("  Removed %d stale images\n", removed)
		}
	}

	var indexed, skipped int
	for _, f := range files {
		existing, err := idx.db.GetDocument(col.ID, f.Path)
		if err == nil && existing.ContentHash == f.ContentHash {
			skipped++
			continue
		}

		docID, err := idx.db.UpsertDocument(col.ID, f.Path, f.Name, f.ContentHash, f.Mtime, 0)
		if err != nil {
			continue
		}

		idx.db.DeleteChunksForDocument(docID)
		idx.db.InsertImageChunk(docID, 0, f.Name, f.Path, nil)
		indexed++
	}

	idx.log.Printf("  Synced: %d indexed, %d unchanged\n", indexed, skipped)
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
	if existingPaths, err := idx.db.ListDocumentPaths(col.ID); err == nil {
		removed := 0
		for path, docID := range existingPaths {
			if !diskPaths[path] {
				idx.db.DeleteDocument(docID)
				removed++
			}
		}
		if removed > 0 {
			idx.log.Printf("  Removed %d stale PDFs\n", removed)
		}
	}

	var ocr source.TextExtractor
	if idx.cfg.Config.OCR.Enabled && idx.cfg.Config.OCR.APIKey != "" {
		ocr = embed.NewOCRClient(idx.cfg.Config.OCR.BaseURL, idx.cfg.Config.OCR.APIKey, idx.cfg.Config.OCR.Model)
	}

	var indexed, skipped int
	for _, f := range files {
		existing, err := idx.db.GetDocument(col.ID, f.Path)
		if err == nil && existing.ContentHash == f.ContentHash {
			skipped++
			continue
		}

		pages, err := source.RasterizePDF(f.Path, idx.cfg.CacheDir, 150, ocr)
		if err != nil {
			continue
		}

		docID, err := idx.db.UpsertDocument(col.ID, f.Path, f.Name, f.ContentHash, f.Mtime, len(pages))
		if err != nil {
			continue
		}

		idx.db.DeleteChunksForDocument(docID)

		var pageText strings.Builder
		for _, pg := range pages {
			content := fmt.Sprintf("PDF page %d of %s", pg.Seq+1, f.Name)
			if pg.Text != "" {
				content += "\n" + pg.Text
				pageText.WriteString(pg.Text)
				pageText.WriteString("\n")
			}
			idx.db.InsertImageChunk(docID, pg.Seq, content, pg.Path, nil)
		}

		if pageText.Len() > 0 {
			idx.db.UpsertFTS(docID, f.Name, pageText.String())
		}

		indexed++
		idx.log.Printf("  Indexed %s (%d pages)\n", f.Name, len(pages))
	}

	idx.log.Printf("PDFs: %d indexed, %d skipped\n", indexed, skipped)
	return nil
}
