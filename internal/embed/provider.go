package embed

import (
	"context"
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
