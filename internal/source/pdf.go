package source

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/seek/internal/config"
	"github.com/gen2brain/go-fitz"
)

// PdfFile represents a PDF found on disk (before page rasterization).
type PdfFile struct {
	Path        string
	Name        string // filename without extension
	ContentHash string
	Mtime       float64
}

// TextExtractor extracts text from a base64 data-URI image. Implemented by
// embed.OCRClient; defined here so source does not depend on embed.
type TextExtractor interface {
	ExtractText(imageDataURI string) (string, error)
}

// PageImage is one rasterized PDF page rendered to a cached PNG file.
type PageImage struct {
	Path string // absolute path to the cached PNG
	Seq  int    // 0-based page index
	Text string // extracted text (embedded or OCR'd); empty if none
}

// ScanPdfs walks a directory and returns all PDF files.
func ScanPdfs(dir string) ([]PdfFile, error) {
	var files []PdfFile
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".pdf" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		hash := sha256.Sum256(data)
		base := filepath.Base(path)
		name := strings.TrimSuffix(base, filepath.Ext(base))
		files = append(files, PdfFile{
			Path:        path,
			Name:        name,
			ContentHash: fmt.Sprintf("%x", hash),
			Mtime:       float64(info.ModTime().Unix()),
		})
		return nil
	})
	return files, err
}

// RasterizePDF renders every page of a PDF to a PNG in cacheDir/pdf/<hash>/ and
// returns the page images. Pages are rendered at 150 DPI, which gives VL models
// clear page images without exploding token/byte sizes.
//
// Text is extracted per page: embedded text is used when present; otherwise, if
// ocr is non-nil, the rendered page is OCR'd. ocr may be nil to skip OCR.
func RasterizePDF(pdfPath, cacheDir string, dpi float64, ocr TextExtractor) ([]PageImage, error) {
	if dpi <= 0 {
		dpi = config.DefaultPDFDPI
	}
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("read pdf %s: %w", pdfPath, err)
	}
	hash := sha256.Sum256(data)
	dir := filepath.Join(cacheDir, "pdf", fmt.Sprintf("%x", hash))
	if err := os.MkdirAll(dir, config.DefaultDirPerms); err != nil {
		return nil, fmt.Errorf("create pdf cache %s: %w", dir, err)
	}

	doc, err := fitz.New(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("open pdf %s: %w", pdfPath, err)
	}
	defer doc.Close()

	pages := make([]PageImage, 0, doc.NumPage())
	for i := 0; i < doc.NumPage(); i++ {
		png, err := doc.ImagePNG(i, dpi)
		if err != nil {
			return nil, fmt.Errorf("render page %d of %s: %w", i, pdfPath, err)
		}
		pagePath := filepath.Join(dir, fmt.Sprintf("page_%04d.png", i))
		if err := os.WriteFile(pagePath, png, config.DefaultFilePerms); err != nil {
			return nil, fmt.Errorf("write page png %s: %w", pagePath, err)
		}

		text, _ := doc.Text(i)
		text = strings.TrimSpace(text)
		if text == "" && ocr != nil {
			text, _ = ocr.ExtractText(dataURIFromFile(pagePath))
		}

		pages = append(pages, PageImage{Path: pagePath, Seq: i, Text: text})
	}
	return pages, nil
}

// dataURIFromFile reads a file and returns it as a base64 data URI (image/png).
func dataURIFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}
