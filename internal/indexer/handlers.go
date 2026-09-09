package indexer

import (
	"context"

	"github.com/ozgurulukir/seek/internal/store"
)

// HandlerDeps contains the injected dependencies shared by source handlers.
// The writer is deliberately explicit at the dispatch boundary so a handler
// cannot silently create a second persistence path.
type HandlerDeps struct {
	Indexer *Indexer
	Writer  IndexWriter
}

// SourceHandler is the typed dispatch boundary for one collection format.
// Context is explicit so handlers cannot accidentally fall back to a process-
// global background context when invoked by a command or hook.
type SourceHandler func(context.Context, *HandlerDeps, *store.Collection) error

func bindSourceHandler(fn func(*Indexer, *store.Collection) error) SourceHandler {
	return func(ctx context.Context, deps *HandlerDeps, col *store.Collection) error {
		deps.Indexer.WithContext(ctx)
		return fn(deps.Indexer, col)
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
