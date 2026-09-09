package indexer

import (
	"fmt"
	"strings"
	"time"

	"github.com/ozgurulukir/seek/internal/chunk"
	"github.com/ozgurulukir/seek/internal/source"
	"github.com/ozgurulukir/seek/internal/source/parserdef"
	"github.com/ozgurulukir/seek/internal/store"
)

// syncParserDef owns schema-driven source matching and its session adapter.
// Persistence still goes through the same transactional writer as native
// formats, so parser-specific concerns stop at this file.
func (idx *Indexer) syncParserDef(col *store.Collection) error {
	if err := idx.ctx().Err(); err != nil {
		return err
	}
	definition, err := parserdef.Load(col.ParserName)
	if err != nil {
		return fmt.Errorf("load parser %q: %w", col.ParserName, err)
	}
	src, version, files, err := definition.MatchContext(idx.ctx())
	if err != nil {
		return fmt.Errorf("detect source for %q: %w", col.ParserName, err)
	}

	reindex := col.ParserVersion != version.Version
	var since time.Time
	if reindex {
		idx.log.Printf("  Parser %q version changed: %d → %d (full reindex)\n", col.ParserName, col.ParserVersion, version.Version)
		if err := idx.reindexParserCollection(col); err != nil {
			return fmt.Errorf("reindex: %w", err)
		}
		if err := idx.db.UpdateCollectionParserVersionContext(idx.ctx(), col.ID, version.Version); err != nil {
			return fmt.Errorf("update parser version: %w", err)
		}
		col.ParserVersion = version.Version
	} else {
		maxMtime, err := idx.db.MaxDocumentMtimeContext(idx.ctx(), col.ID)
		if err != nil {
			return fmt.Errorf("read parser cursor: %w", err)
		}
		if maxMtime > 0 {
			since = time.UnixMilli(int64(maxMtime))
		}
	}

	sessions, sessionErrors, err := parserdef.SyncSessionsContext(idx.ctx(), src, version, files, since)
	if err != nil {
		return fmt.Errorf("sync sessions: %w", err)
	}
	for _, sessionError := range sessionErrors {
		idx.warnf("  WARN: session %s: %v\n", sessionError.SessionID, sessionError.Err)
		idx.recordFailure(sessionError.SessionID, "scan", sessionError.Err)
	}

	var indexed, skipped, failed int
	seenPaths := make(map[string]bool)
	for _, session := range sessions {
		if err := idx.ctx().Err(); err != nil {
			return err
		}
		docPath := session.SrcPath + "#" + session.ID
		seenPaths[docPath] = true
		if session.Messages == nil || len(session.Messages) == 0 {
			skipped++
			continue
		}

		title := session.Title
		if title == "" {
			for _, message := range session.Messages {
				if message.Role == source.RoleUser {
					title = source.Truncate(message.Content, source.TitleMaxLen)
					break
				}
			}
		}
		if title == "" {
			title = session.ID
		}
		text := parserMessagesToText(session.Messages)
		cursorUnix := 0.0
		if !session.Cursor.IsZero() {
			cursorUnix = float64(session.Cursor.UnixMilli())
		}
		maxSize, _ := idx.chunkSize()
		_, err = idx.writer.UpsertAndReplaceIndex(idx.ctx(), store.DocumentIndex{
			CollectionID: col.ID,
			Path:         docPath,
			Title:        title,
			Mtime:        cursorUnix,
			LineCount:    len(session.Messages),
			FTSContent:   text,
			Chunks:       toIndexChunks(chunk.ChunkConversation(text, maxSize), false),
			FastFields:   session.Metadata,
		})
		if err != nil {
			idx.warnf("  WARN: index %s: %v\n", docPath, err)
			failed++
			continue
		}
		indexed++
	}

	if len(sessionErrors) == 0 {
		if _, err := idx.cleanupOrphans(col.ID, seenPaths, "sessions"); err != nil {
			return fmt.Errorf("cleanup sessions: %w", err)
		}
	} else {
		idx.warnf("  WARN: skipping orphan cleanup due to %d scan error(s)\n", len(sessionErrors))
	}
	idx.addReport(SyncReport{Indexed: indexed, Skipped: skipped, Failed: failed + len(sessionErrors)})
	idx.log.Printf("  Synced: %d indexed, %d unchanged", indexed, skipped)
	if failed > 0 || len(sessionErrors) > 0 {
		idx.log.Printf(", %d failed", failed+len(sessionErrors))
	}
	idx.log.Printf("\n")
	return nil
}

func (idx *Indexer) reindexParserCollection(col *store.Collection) error {
	_, err := idx.cleanupOrphans(col.ID, nil, "")
	return err
}

func parserMessagesToText(messages []parserdef.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		fmt.Fprintf(&builder, "[%s]: %s\n\n", message.Role, message.Content)
	}
	return builder.String()
}
