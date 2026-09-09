package indexer

import (
	"context"

	"github.com/ozgurulukir/seek/internal/store"
)

// IndexWriter is the transactional persistence dependency shared by source
// handlers. Keeping it narrower than Store lets handler tests force write
// failures without constructing SQLite internals.
type IndexWriter interface {
	UpsertAndReplaceIndex(context.Context, store.DocumentIndex) (int64, error)
	UpsertAndAppendIndex(context.Context, store.DocumentIndex) (int64, error)
}
