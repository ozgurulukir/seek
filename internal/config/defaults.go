package config

import "time"

const (
	// DefaultEmbeddingBaseURL is the fallback endpoint for text embeddings.
	DefaultEmbeddingBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	// DefaultEmbeddingModel is the fallback model for text embeddings.
	DefaultEmbeddingModel = "text-embedding-v4"
	// DefaultEmbeddingDimensions is the fallback dimension count for text embeddings.
	DefaultEmbeddingDimensions = 1024

	// DefaultVLBaseURL is the DashScope multimodal embedding endpoint.
	DefaultVLBaseURL = "https://dashscope.aliyuncs.com/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding"

	// DefaultEmbeddingTimeout is the default timeout for text embedding requests.
	DefaultEmbeddingTimeout = 60 * time.Second
	// DefaultVLTimeout is the default timeout for multimodal embedding requests.
	DefaultVLTimeout = 120 * time.Second
)

const (
	// DefaultDirPerms is the default permission for created directories.
	DefaultDirPerms = 0755
	// DefaultFilePerms is the default permission for created files.
	DefaultFilePerms = 0644

	// DefaultTextSnippetLen is the length for text results in search.
	DefaultTextSnippetLen = 200
	// DefaultImageSnippetLen is the length for image context results in search.
	DefaultImageSnippetLen = 150

	// DefaultEmbeddingBatchSize is the default batch size for text embeddings.
	DefaultEmbeddingBatchSize = 6
	// DefaultVLMaxContents is the max contents per multimodal request.
	DefaultVLMaxContents = 20
	// DefaultVLMaxImages is the max images per multimodal request.
	DefaultVLMaxImages = 5
	// DefaultBatchPollInterval is the time between polling batch job status.
	DefaultBatchPollInterval = 5 * time.Second

	// DefaultPDFDPI is the default DPI for PDF rasterization.
	DefaultPDFDPI = 150.0
	// DefaultOCRModel is the default model for OCR extraction.
	DefaultOCRModel = "qwen-vl-ocr"
)
