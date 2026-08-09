// Package xberg is the remote extraction backend for the extractor domain.
// It talks to an xberg serve HTTP API (default http://127.0.0.1:8000) which
// extracts text from 100+ document formats (docx/xlsx/pptx/epub/html/eml/csv/
// ...). This package depends only on the standard library + config, keeping
// the extraction domain free of cgo/heavy native deps.
package xberg

// extractionResult mirrors the xberg /extract JSON response envelope.
// Field names are snake_case as serialized by the xberg Rust core
// (crates/xberg/src/types/extraction.rs); there is no serde rename_all.
type extractionResult struct {
	Results []extractedDocument `json:"results"`
	Errors  []extractionError   `json:"errors"`
	Summary extractionSummary   `json:"summary"`
}

// extractedDocument is one extracted input. Only the fields seek uses are
// modeled; the full schema (tables, chunks, images, metadata, ...) is ignored.
type extractedDocument struct {
	// Content is the extracted text in the requested output_format.
	Content string `json:"content"`
	// MimeType is the detected MIME type of the source input.
	MimeType string `json:"mime_type"`
}

type extractionError struct {
	Input   string `json:"input,omitempty"`
	Message string `json:"message,omitempty"`
}

type extractionSummary struct {
	Inputs  int `json:"inputs"`
	Results int `json:"results"`
	Errors  int `json:"errors"`
}

// healthResponse is the body of GET /health.
type healthResponse struct {
	Status string `json:"status"`
}

// supportedFormat is one entry of GET /formats.
type supportedFormat struct {
	Extension string `json:"extension"`
	MimeType  string `json:"mime_type"`
}
