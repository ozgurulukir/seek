package config

import (
	"testing"
	"time"
)

func TestDefaultConstantsSanity(t *testing.T) {
	// Embedding & VL Defaults
	if DefaultEmbeddingBaseURL == "" {
		t.Error("DefaultEmbeddingBaseURL should not be empty")
	}
	if DefaultEmbeddingModel == "" {
		t.Error("DefaultEmbeddingModel should not be empty")
	}
	if DefaultEmbeddingDimensions <= 0 {
		t.Errorf("DefaultEmbeddingDimensions = %d, want > 0", DefaultEmbeddingDimensions)
	}
	if DefaultVLBaseURL == "" {
		t.Error("DefaultVLBaseURL should not be empty")
	}
	if DefaultEmbeddingTimeout <= 0 {
		t.Errorf("DefaultEmbeddingTimeout = %v, want > 0", DefaultEmbeddingTimeout)
	}
	if DefaultVLTimeout <= 0 {
		t.Errorf("DefaultVLTimeout = %v, want > 0", DefaultVLTimeout)
	}

	// Embedding modes
	if ModeAuto != "auto" {
		t.Errorf("ModeAuto = %q, want %q", ModeAuto, "auto")
	}
	if ModeRealtime != "realtime" {
		t.Errorf("ModeRealtime = %q, want %q", ModeRealtime, "realtime")
	}
	if ModeBatch != "batch" {
		t.Errorf("ModeBatch = %q, want %q", ModeBatch, "batch")
	}

	// File / Directory permissions
	if DefaultDirPerms != 0755 {
		t.Errorf("DefaultDirPerms = %04o, want 0755", DefaultDirPerms)
	}
	if DefaultFilePerms != 0644 {
		t.Errorf("DefaultFilePerms = %04o, want 0644", DefaultFilePerms)
	}
	if DefaultPrivateDirPerms != 0700 {
		t.Errorf("DefaultPrivateDirPerms = %04o, want 0700", DefaultPrivateDirPerms)
	}
	if DefaultPrivateFilePerms != 0600 {
		t.Errorf("DefaultPrivateFilePerms = %04o, want 0600", DefaultPrivateFilePerms)
	}

	// Snippets & Chunks
	if DefaultTextSnippetLen <= 0 {
		t.Errorf("DefaultTextSnippetLen = %d, want > 0", DefaultTextSnippetLen)
	}
	if DefaultImageSnippetLen <= 0 {
		t.Errorf("DefaultImageSnippetLen = %d, want > 0", DefaultImageSnippetLen)
	}
	if DefaultChunkMaxSize <= 0 {
		t.Errorf("DefaultChunkMaxSize = %d, want > 0", DefaultChunkMaxSize)
	}
	if DefaultChunkOverlap < 0 || DefaultChunkOverlap >= DefaultChunkMaxSize {
		t.Errorf("DefaultChunkOverlap = %d, want >= 0 and < DefaultChunkMaxSize (%d)", DefaultChunkOverlap, DefaultChunkMaxSize)
	}

	// Batch & Content Limits
	if DefaultEmbeddingBatchSize <= 0 {
		t.Errorf("DefaultEmbeddingBatchSize = %d, want > 0", DefaultEmbeddingBatchSize)
	}
	if DefaultVLMaxContents <= 0 {
		t.Errorf("DefaultVLMaxContents = %d, want > 0", DefaultVLMaxContents)
	}
	if DefaultVLMaxImages <= 0 {
		t.Errorf("DefaultVLMaxImages = %d, want > 0", DefaultVLMaxImages)
	}
	if DefaultBatchPollInterval <= 0 {
		t.Errorf("DefaultBatchPollInterval = %v, want > 0", DefaultBatchPollInterval)
	}

	// OCR & PDF
	if DefaultPDFDPI <= 0 {
		t.Errorf("DefaultPDFDPI = %v, want > 0", DefaultPDFDPI)
	}
	if DefaultOCRModel == "" {
		t.Error("DefaultOCRModel should not be empty")
	}
	if DefaultOCRMaxTokens <= 0 {
		t.Errorf("DefaultOCRMaxTokens = %d, want > 0", DefaultOCRMaxTokens)
	}

	// Rerank
	if DefaultRerankModel == "" {
		t.Error("DefaultRerankModel should not be empty")
	}
	if DefaultRerankTopN <= 0 {
		t.Errorf("DefaultRerankTopN = %d, want > 0", DefaultRerankTopN)
	}

	// Search & Query Defaults
	if DefaultQueryMode == "" {
		t.Error("DefaultQueryMode should not be empty")
	}
	if DefaultSearchLimit <= 0 {
		t.Errorf("DefaultSearchLimit = %d, want > 0", DefaultSearchLimit)
	}
	if DefaultAnalyzeLang == "" {
		t.Error("DefaultAnalyzeLang should not be empty")
	}
	if DefaultRRFK <= 0 {
		t.Errorf("DefaultRRFK = %d, want > 0", DefaultRRFK)
	}

	// Vector Index
	if DefaultVectorIndexBackend == "" {
		t.Error("DefaultVectorIndexBackend should not be empty")
	}
	if DefaultHNSWM <= 0 {
		t.Errorf("DefaultHNSWM = %d, want > 0", DefaultHNSWM)
	}
	if DefaultHNSEFConstruction <= 0 {
		t.Errorf("DefaultHNSEFConstruction = %d, want > 0", DefaultHNSEFConstruction)
	}
	if DefaultHNSEFSearch <= 0 {
		t.Errorf("DefaultHNSEFSearch = %d, want > 0", DefaultHNSEFSearch)
	}
	if DefaultHNSWDimension <= 0 {
		t.Errorf("DefaultHNSWDimension = %d, want > 0", DefaultHNSWDimension)
	}

	// Compression
	if DefaultCompressionAlgorithm == "" {
		t.Error("DefaultCompressionAlgorithm should not be empty")
	}
	if DefaultCompressionLevel < 0 {
		t.Errorf("DefaultCompressionLevel = %d, want >= 0", DefaultCompressionLevel)
	}

	// Extractor & Semantic
	if DefaultExtractorBackend == "" {
		t.Error("DefaultExtractorBackend should not be empty")
	}
	if DefaultXbergBaseURL == "" {
		t.Error("DefaultXbergBaseURL should not be empty")
	}
	if DefaultXbergTimeout <= 0 {
		t.Errorf("DefaultXbergTimeout = %v, want > 0", DefaultXbergTimeout)
	}
	if DefaultSemanticBaseURL == "" {
		t.Error("DefaultSemanticBaseURL should not be empty")
	}
	if DefaultSemanticMaxTags <= 0 {
		t.Errorf("DefaultSemanticMaxTags = %d, want > 0", DefaultSemanticMaxTags)
	}
	if DefaultSemanticTimeout <= 0 {
		t.Errorf("DefaultSemanticTimeout = %v, want > 0", DefaultSemanticTimeout)
	}
	if DefaultExtractorOutputFormat == "" {
		t.Error("DefaultExtractorOutputFormat should not be empty")
	}
}

func TestDefaultTimeoutsSanity(t *testing.T) {
	// Verify timeouts are reasonable durations
	if DefaultEmbeddingTimeout < 1*time.Second || DefaultEmbeddingTimeout > 10*time.Minute {
		t.Errorf("DefaultEmbeddingTimeout = %v, out of expected range", DefaultEmbeddingTimeout)
	}
	if DefaultVLTimeout < 1*time.Second || DefaultVLTimeout > 10*time.Minute {
		t.Errorf("DefaultVLTimeout = %v, out of expected range", DefaultVLTimeout)
	}
	if DefaultXbergTimeout < 1*time.Second || DefaultXbergTimeout > 10*time.Minute {
		t.Errorf("DefaultXbergTimeout = %v, out of expected range", DefaultXbergTimeout)
	}
	if DefaultSemanticTimeout < 1*time.Second || DefaultSemanticTimeout > 10*time.Minute {
		t.Errorf("DefaultSemanticTimeout = %v, out of expected range", DefaultSemanticTimeout)
	}
}
