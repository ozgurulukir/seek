package indexer

import (
	"fmt"
	"strings"

	"github.com/ozgurulukir/seek/internal/chunk"
	"github.com/ozgurulukir/seek/internal/extractor"
	"github.com/ozgurulukir/seek/internal/source"
	"github.com/ozgurulukir/seek/internal/store"
)

// syncDocuments owns the rich-document handler: scan, extractor capability
// checks, and the shared transactional writer remain behind this format seam.
func (idx *Indexer) syncDocuments(col *store.Collection) error {
	files, scanIssues, err := source.ScanDocumentsContext(idx.ctx(), col.Path)
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
		switch status := idx.syncDocumentFile(col, f, ext); status {
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
		FastFields:   semanticTagMap(idx.semanticTags(idx.ctx(), f.Path, res.Content)),
	}); err != nil {
		idx.warnf("  WARN: index %s: %v\n", f.Path, err)
		idx.recordFailure(f.Path, "persistence", err)
		return docStatusFailed
	}
	return docStatusIndexed
}
