package indexer

import (
	"fmt"
	"path/filepath"

	"github.com/ozgurulukir/seek/internal/chunk"
	"github.com/ozgurulukir/seek/internal/source"
	"github.com/ozgurulukir/seek/internal/store"
)

// syncCode owns source-code scanning and the code-specific fast-field policy.
func (idx *Indexer) syncCode(col *store.Collection) error {
	files, skippedPaths, err := source.ScanCodeWithWarningsContext(idx.ctx(), col.Path, col.Pattern)
	if err != nil {
		return err
	}
	for _, path := range skippedPaths {
		idx.warnf("  WARN: scan skipped %s (unreadable)\n", path)
		idx.recordFailure(path, "scan", fmt.Errorf("unreadable path"))
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
	for _, file := range files {
		if err := idx.ctx().Err(); err != nil {
			return err
		}
		isSkipped, err := idx.indexCodeFile(col, file)
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

func (idx *Indexer) indexCodeFile(col *store.Collection, file source.CodeFileInfo) (bool, error) {
	existing, err := idx.db.GetDocumentContext(idx.ctx(), col.ID, file.Path)
	if err == nil && existing.ContentHash == file.ContentHash {
		if existing.Mtime != file.Mtime {
			if err := idx.db.UpdateDocumentMtimeContext(idx.ctx(), existing.ID, file.Mtime); err != nil {
				return false, fmt.Errorf("update code mtime: %w", err)
			}
		}
		return true, nil
	}

	maxSize, overlap := idx.chunkSize()
	_, err = idx.writer.UpsertAndReplaceIndex(idx.ctx(), store.DocumentIndex{
		CollectionID: col.ID,
		Path:         file.Path,
		Title:        file.Title,
		ContentHash:  file.ContentHash,
		Mtime:        file.Mtime,
		LineCount:    file.LineCount,
		FTSContent:   file.Content,
		Chunks:       toIndexChunks(chunk.ChunkCode(file.Content, file.Language, maxSize, overlap), true),
		FastFields: map[string]string{
			"lang":     file.Language,
			"ext":      file.Extension,
			"filename": filepath.Base(file.Path),
			"rel_path": file.RelativePath,
			"repo":     col.Name,
		},
	})
	if err != nil {
		return false, fmt.Errorf("index %s: %w", file.Path, err)
	}
	return false, nil
}
