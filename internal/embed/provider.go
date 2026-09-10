package embed

import (
	"context"
	"reflect"
	"time"
)

// Provider is the capability bundle owned by the application composition
// root. Keeping capabilities as interfaces lets search and pipeline depend on
// what they use while one runtime still owns the concrete clients.
type Provider struct {
	Query    QueryEmbedder
	Document DocumentEmbedder
	Batch    BatchEmbedder
	VLQuery  VLQueryEmbedder
	VLText   VLTextBatcher
	VLImage  VLImageBatcher
	Reranker Reranker
}

// NormalizeCapabilities converts typed-nil capability values into nil
// interfaces. Provider factories are extensible, so checking only
// capability != nil is insufficient: a nil pointer stored in an interface is
// itself non-nil and would otherwise panic when invoked.
//
// Keep this defensive check at the provider boundary. Consumers can use
// ordinary interface nil checks and do not need to know concrete provider
// implementations.
func (p Provider) NormalizeCapabilities() Provider {
	if isNilCapability(p.Query) {
		p.Query = nil
	}
	if isNilCapability(p.Document) {
		p.Document = nil
	}
	if isNilCapability(p.Batch) {
		p.Batch = nil
	}
	if isNilCapability(p.VLQuery) {
		p.VLQuery = nil
	}
	if isNilCapability(p.VLText) {
		p.VLText = nil
	}
	if isNilCapability(p.VLImage) {
		p.VLImage = nil
	}
	if isNilCapability(p.Reranker) {
		p.Reranker = nil
	}
	return p
}

func isNilCapability(capability any) bool {
	if capability == nil {
		return true
	}
	value := reflect.ValueOf(capability)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Capability interfaces for the embedding subsystem. Consumers (search
// Engine, pipeline, cmd) depend on these rather than the concrete clients so
// mock providers can substitute any backend in tests (M7).
//
// Reranker lives in rerank.go; extractor.OCR in internal/extractor covers the
// OCR capability without an embed import.

// QueryEmbedder embeds a single search query text.
type QueryEmbedder interface {
	EmbedQuery(text string) ([]float32, error)
}

type ContextQueryEmbedder interface {
	EmbedQueryContext(ctx context.Context, text string) ([]float32, error)
}

// VLQueryEmbedder is the multimodal counterpart of QueryEmbedder. VLClient
// and *Client (when a multimodal model is configured) both satisfy it.
type VLQueryEmbedder interface {
	EmbedText(text string) ([]float32, error)
}

type ContextVLQueryEmbedder interface {
	EmbedTextContext(ctx context.Context, text string) ([]float32, error)
}

// DocumentEmbedder embeds a batch of documents synchronously.
type DocumentEmbedder interface {
	EmbedDocuments(texts []string) ([][]float32, error)
}

type ContextDocumentEmbedder interface {
	EmbedDocumentsContext(ctx context.Context, texts []string) ([][]float32, error)
}

// BatchEmbedder submits texts to an asynchronous batch API. The onProgress
// callback receives (status, elapsed) updates while the batch runs.
type BatchEmbedder interface {
	BatchEmbedAsync(texts []string, onProgress func(status string, elapsed time.Duration)) ([][]float32, error)
}

type ContextBatchEmbedder interface {
	BatchEmbedAsyncContext(ctx context.Context, texts []string, onProgress func(status string, elapsed time.Duration)) ([][]float32, error)
}

// VLTextBatcher embeds plain texts through the VL realtime API in batches.
// The callback receives per-batch embeddings indexed from batchStart.
type VLTextBatcher interface {
	EmbedTextsInBatches(texts []string, batchSize int, pause time.Duration, onBatch func(batchStart int, embeddings [][]float32) error) (int, error)
}

type ContextVLTextBatcher interface {
	EmbedTextsInBatchesContext(ctx context.Context, texts []string, batchSize int, pause time.Duration, onBatch func(batchStart int, embeddings [][]float32) error) (int, error)
}

// VLImageBatcher embeds image+text pairs through the VL realtime API.
// validIndices maps each returned embedding back to its item index.
type VLImageBatcher interface {
	EmbedImagesInBatches(items []ImageBatchItem, batchSize int, pause time.Duration, onBatch func(batchStart int, embeddings [][]float32, validIndices []int) error) (int, error)
}

type ContextVLImageBatcher interface {
	EmbedImagesInBatchesContext(ctx context.Context, items []ImageBatchItem, batchSize int, pause time.Duration, onBatch func(batchStart int, embeddings [][]float32, validIndices []int) error) (int, error)
}
