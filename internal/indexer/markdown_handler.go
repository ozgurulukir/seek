package indexer

import (
	"fmt"

	"github.com/ozgurulukir/seek/internal/chunk"
	"github.com/ozgurulukir/seek/internal/source"
	"github.com/ozgurulukir/seek/internal/store"
)

func (idx *Indexer) syncMarkdown(col *store.Collection) error {
	files, scanIssues, err := source.ScanMarkdownContext(idx.ctx(), col.Path, col.Pattern)
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
