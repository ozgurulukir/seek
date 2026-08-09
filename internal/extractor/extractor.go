// Package extractor is the extraction domain: it turns a file on disk into
// indexable text (and, for page-oriented formats like PDF, optional page
// images for VL embedding). The package is independent of the indexer and
// embedding layers — it depends only on config and the standard library.
//
// Two backends are provided as subpackages:
//   - builtin: native Go extractors (markdown text read, PDF rasterization via
//     go-fitz with optional OCR, images passed through for VL embedding).
//   - xberg:   a remote REST extraction service (xberg serve) that handles
//     100+ document formats (docx/xlsx/pptx/epub/html/eml/csv/...).
//
// To avoid an import cycle (both backends import this package for the
// interface), the backend is constructed where it is wired in (the indexer),
// not in a factory here. See indexer.NewExtractor for the config→backend
// dispatch. The active backend is selected globally via
// config.ExtractorConfig.Backend ("builtin" by default) and may be overridden
// per-command with --backend.
package extractor

import (
	"context"
	"errors"
)

// Result is the output of extracting a single file.
type Result struct {
	// Content is the extracted text (markdown for document formats, raw text
	// for markdown/plain). Empty for image-only results.
	Content string
	// MimeType is the detected/declared MIME type of the source file.
	MimeType string
	// Title is a best-effort document title; empty if none could be determined.
	Title string
	// Pages holds page images for page-oriented formats (PDF). Each page has a
	// cached PNG path and any extracted text. Empty for non-paged formats and
	// for backends that only return text.
	Pages []Page
}

// Page is one page of a page-oriented document (e.g. a rasterized PDF page).
type Page struct {
	// Path is the absolute path to a cached page image (PNG). Empty if the
	// backend did not render a page image (text-only extraction).
	Path string
	// Seq is the 0-based page index.
	Seq int
	// Text is the text extracted for this page (embedded or OCR'd); may be empty.
	Text string
}

// Extractor turns a single on-disk file into a Result.
//
// Implementations must be safe for concurrent use. A backend that does not
// handle the given file type should return false from Supports rather than
// erroring from Extract; callers use Supports to filter directories.
type Extractor interface {
	// Extract reads the file at path and returns its extracted content.
	Extract(ctx context.Context, path string) (Result, error)
	// Supports reports whether this backend handles the file at path (by
	// extension and/or MIME sniff).
	Supports(path string) bool
	// Name is the backend identifier ("builtin", "xberg").
	Name() string
}

// OCR extracts text from a base64 data-URI image. It is implemented by
// embed.OCRClient and defined here so the builtin PDF extractor can depend on
// the capability without importing embed (the indexer injects the concrete
// client). Mirrors source.TextExtractor, which this supersedes.
type OCR interface {
	ExtractText(imageDataURI string) (string, error)
}

// ErrUnsupported is returned by Extract when a backend is asked to handle a
// file it does not support. Callers should filter with Supports first; this is
// a safety net.
var ErrUnsupported = errors.New("extractor: file type not supported by backend")
