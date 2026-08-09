// Package builtin is the native Go extraction backend for the extractor domain.
// It handles the formats seek originally shipped with, without any external
// service:
//   - markdown: read text directly (title from first "# " heading).
//   - PDF:      rasterize each page to a cached PNG via go-fitz (bundled
//     static MuPDF, cgo), extracting embedded text per page and
//     falling back to the injected OCR client for scanned pages.
//   - images:   passed through (no text extraction); the indexer embeds them
//     via the VL client. Supports returns true so the builtin
//     backend can serve image collections exactly as before.
//
// The OCR dependency is injected as an extractor.OCR so this package never
// imports embed (the indexer wires embed.OCRClient in). This preserves the
// layering rule that the extraction domain is independent of embedding.
package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gen2brain/go-fitz"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/extractor"
)

// Extractor is the builtin backend. It dispatches per file extension to the
// matching native routine. It is safe for concurrent use (go-fitz opens a new
// document per call; the OCR client is responsible for its own concurrency).
type Extractor struct {
	ocr      extractor.OCR
	cacheDir string
}

// New builds a builtin extractor. ocr may be nil (OCR is then skipped for
// scanned PDFs, matching the original source.RasterizePDF(nil) behavior).
// cacheDir is where rasterized PDF page PNGs are written.
func New(ocr extractor.OCR, cacheDir string) *Extractor {
	return &Extractor{ocr: ocr, cacheDir: cacheDir}
}

// Name implements extractor.Extractor.
func (e *Extractor) Name() string { return "builtin" }

// Supports reports whether path is a format the builtin backend handles.
// Note: markdown/images/pdf only. Rich document formats (docx/xlsx/...) are
// intentionally not supported here — use the xberg backend for those.
func (e *Extractor) Supports(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown":
		return true
	case ".pdf":
		return true
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".tiff", ".tif", ".svg":
		return true
	}
	return false
}

// Extract reads path and returns its content. For markdown it returns the raw
// text; for PDF it returns the concatenated page text and the page images in
// Result.Pages (for VL embedding); for images it returns an empty content with
// the path (the indexer embeds the file directly).
func (e *Extractor) Extract(ctx context.Context, path string) (extractor.Result, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown":
		return e.extractMarkdown(path)
	case ".pdf":
		return e.extractPDF(ctx, path)
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".tiff", ".tif", ".svg":
		return e.passThroughImage(path), nil
	}
	return extractor.Result{}, fmt.Errorf("%w: %s", extractor.ErrUnsupported, path)
}

// extractMarkdown reads the file and derives a title from the first "# " heading.
func (e *Extractor) extractMarkdown(path string) (extractor.Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return extractor.Result{}, fmt.Errorf("builtin: read %s: %w", path, err)
	}
	content := string(data)
	return extractor.Result{
		Content:  content,
		MimeType: "text/markdown",
		Title:    markdownTitle(content, path),
	}, nil
}

// passThroughImage returns an empty-content result carrying the path. The
// indexer reads the file directly for VL embedding, mirroring the original
// InsertImageChunk behavior (which took the path, not extracted text).
func (e *Extractor) passThroughImage(path string) extractor.Result {
	return extractor.Result{
		Content:  "",
		MimeType: imageMIME(strings.ToLower(filepath.Ext(path))),
		Title:    titleFromPath(path),
	}
}

// extractPDF rasterizes each page to a cached PNG and extracts text (embedded
// or OCR'd). The concatenated page text becomes Result.Content; each page is
// also returned in Result.Pages so the indexer can store page image chunks.
func (e *Extractor) extractPDF(ctx context.Context, pdfPath string) (extractor.Result, error) {
	// Respect cancellation before the (potentially long) rasterization loop.
	select {
	case <-ctx.Done():
		return extractor.Result{}, ctx.Err()
	default:
	}

	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return extractor.Result{}, fmt.Errorf("builtin: read pdf %s: %w", pdfPath, err)
	}
	hash := sha256.Sum256(data)
	dir := filepath.Join(e.cacheDir, "pdf", fmt.Sprintf("%x", hash))
	if err := os.MkdirAll(dir, config.DefaultDirPerms); err != nil {
		return extractor.Result{}, fmt.Errorf("builtin: create pdf cache %s: %w", dir, err)
	}

	doc, err := fitz.New(pdfPath)
	if err != nil {
		return extractor.Result{}, fmt.Errorf("builtin: open pdf %s: %w", pdfPath, err)
	}
	defer doc.Close()

	pages := make([]extractor.Page, 0, doc.NumPage())
	var allText strings.Builder
	for i := 0; i < doc.NumPage(); i++ {
		if err := ctx.Err(); err != nil {
			return extractor.Result{}, err
		}
		png, err := doc.ImagePNG(i, config.DefaultPDFDPI)
		if err != nil {
			return extractor.Result{}, fmt.Errorf("builtin: render page %d of %s: %w", i, pdfPath, err)
		}
		pagePath := filepath.Join(dir, fmt.Sprintf("page_%04d.png", i))
		if err := os.WriteFile(pagePath, png, config.DefaultFilePerms); err != nil {
			return extractor.Result{}, fmt.Errorf("builtin: write page png %s: %w", pagePath, err)
		}

		text, _ := doc.Text(i)
		text = strings.TrimSpace(text)
		if text == "" && e.ocr != nil {
			text, _ = e.ocr.ExtractText(dataURIFromFile(pagePath))
		}
		if text != "" {
			allText.WriteString(text)
			allText.WriteString("\n")
		}
		pages = append(pages, extractor.Page{Path: pagePath, Seq: i, Text: text})
	}

	return extractor.Result{
		Content:  allText.String(),
		MimeType: "application/pdf",
		Title:    titleFromPath(pdfPath),
		Pages:    pages,
	}, nil
}

// dataURIFromFile reads a file and returns it as a base64 data URI (image/png).
func dataURIFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}

// markdownTitle returns the first "# " heading, or the filename stem as a
// fallback. Mirrors source.extractMarkdownTitle.
func markdownTitle(content, path string) string {
	for _, line := range strings.SplitN(content, "\n", 20) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimPrefix(trimmed, "# ")
		}
	}
	return titleFromPath(path)
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// imageMIME maps a lowercase image extension to its MIME type. Used so the
// indexer/embedding layer can route by type without re-sniffing.
func imageMIME(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".tiff", ".tif":
		return "image/tiff"
	case ".svg":
		return "image/svg+xml"
	}
	return "application/octet-stream"
}

// Compile-time interface check.
var _ extractor.Extractor = (*Extractor)(nil)

// newBuiltin is the package-private constructor used by the extractor factory.
func newBuiltin(ocr extractor.OCR, cacheDir string) extractor.Extractor {
	return New(ocr, cacheDir)
}
