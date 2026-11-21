package indexer

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/ozgurulukir/seek/internal/store"
)

// skipUnchanged is the single content-hash skip policy shared by every file
// format handler: load the existing document, compare content hashes, and —
// when unchanged — refresh the stored mtime so sync cursors and `collection
// show` report fresh timestamps. Returns true when the file is unchanged and
// can be skipped; returns false with a nil error when the file must be
// re-indexed. A missing document row means the file is new (re-index);
// any other load error is a persistence failure that aborts the sync pass.
func (idx *Indexer) skipUnchanged(col *store.Collection, path, contentHash string, mtime float64) (bool, error) {
	existing, err := idx.db.GetDocumentContext(idx.ctx(), col.ID, path)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load document %s: %w", path, err)
	}
	if existing.ContentHash != contentHash {
		return false, nil
	}
	if existing.Mtime != mtime {
		if err := idx.db.UpdateDocumentMtimeContext(idx.ctx(), existing.ID, mtime); err != nil {
			return false, fmt.Errorf("update document mtime for %s: %w", path, err)
		}
	}
	return true, nil
}
