package indexer

import (
	"path/filepath"
	"strings"

	"github.com/ozgurulukir/seek/internal/source"
	"github.com/ozgurulukir/seek/internal/store"
)

func (idx *Indexer) syncClaude(col *store.Collection) error {
	return idx.syncConversation(col, func() ([]source.ConversationFile, error) {
		return source.ScanClaudeFilesContext(idx.ctx())
	}, func(path string, fromLine int) (ConversationBatch, error) {
		convID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		msgs, imgs, err := source.ParseClaudeFileWithImagesContext(idx.ctx(), path, fromLine, convID)
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
	}, nil)
}

func (idx *Indexer) syncCodex(col *store.Collection) error {
	threadNames := source.LoadCodexThreadNamesContext(idx.ctx())
	return idx.syncConversation(col, func() ([]source.ConversationFile, error) {
		return source.ScanCodexFilesContext(idx.ctx())
	}, func(path string, fromLine int) (ConversationBatch, error) {
		msgs, sessionID, imgs, err := source.ParseCodexFileWithImagesContext(idx.ctx(), path, fromLine)
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
	}, func(sessionID, defaultTitle string) string {
		if name, ok := threadNames[sessionID]; ok && name != "" {
			return name
		}
		return defaultTitle
	})
}
