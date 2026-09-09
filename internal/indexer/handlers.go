package indexer

import (
	"context"

	"github.com/ozgurulukir/seek/internal/store"
)

// SourceHandler is the typed dispatch boundary for one collection format.
// Context is explicit so handlers cannot accidentally fall back to a process-
// global background context when invoked by a command or hook.
type SourceHandler func(context.Context, *Indexer, *store.Collection) error

func bindSourceHandler(fn func(*Indexer, *store.Collection) error) SourceHandler {
	return func(ctx context.Context, idx *Indexer, col *store.Collection) error {
		idx.WithContext(ctx)
		return fn(idx, col)
	}
}

// syncHandlers is the single registration point for per-format sync. The
// map keys mirror the 1:1 format dispatch previously expressed as a switch.
var syncHandlers = map[store.CollectionType]SourceHandler{
	store.CollectionTypeMarkdown:  bindSourceHandler((*Indexer).syncMarkdown),
	store.CollectionTypeClaude:    bindSourceHandler((*Indexer).syncClaude),
	store.CollectionTypeCodex:     bindSourceHandler((*Indexer).syncCodex),
	store.CollectionTypeImages:    bindSourceHandler((*Indexer).syncImage),
	store.CollectionTypePDF:       bindSourceHandler((*Indexer).syncPdf),
	store.CollectionTypeDocuments: bindSourceHandler((*Indexer).syncDocuments),
	store.CollectionTypeCode:      bindSourceHandler((*Indexer).syncCode),
	store.CollectionTypeParser:    bindSourceHandler((*Indexer).syncParserDef),
}
