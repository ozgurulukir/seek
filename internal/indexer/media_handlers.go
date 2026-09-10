package indexer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ozgurulukir/seek/internal/chunk"
	"github.com/ozgurulukir/seek/internal/source"
	"github.com/ozgurulukir/seek/internal/store"
)

func (idx *Indexer) syncImage(col *store.Collection) error {
	files, scanIssues, err := source.ScanImagesContext(idx.ctx(), col.Path)
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
			FTSContent:   f.Name,
			Chunks:       []store.IndexChunk{{Seq: 0, Content: f.Name, ChunkType: store.ChunkTypeImage, ImagePath: f.Path}},
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
	files, scanIssues, err := source.ScanPdfsContext(idx.ctx(), col.Path)
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
				indexChunks = append(indexChunks, store.IndexChunk{Seq: pg.Seq, Content: cb.String(), ChunkType: store.ChunkTypeImage, ImagePath: pg.Path})
			}
		} else if res.Content != "" {
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
			FastFields:   semanticTagMap(idx.semanticTags(idx.ctx(), f.Name, pageText.String())),
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
