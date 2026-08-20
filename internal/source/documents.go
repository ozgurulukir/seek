package source

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DocumentFile represents a single rich-document file found on disk
// (docx/xlsx/pptx/epub/html/eml/csv/...) — anything the xberg backend handles
// that the builtin markdown/pdf/images scanners do not.
type DocumentFile struct {
	Path        string
	Name        string // filename without extension
	ContentHash string
	Mtime       float64
}

// documentExtensions are the file extensions the documents scanner collects.
// This is intentionally a curated set of the common rich formats xberg handles;
// the extractor backend (builtin/xberg) makes the final Supports decision via
// its own extension/MIME check, so a file picked up here but unsupported by the
// active backend is skipped during indexing (counted as failed/unsupported).
//
// Markdown (.md/.markdown), PDF (.pdf), and the image extensions are excluded
// because they have dedicated collection types (markdown/pdf/images) and the
// builtin backend already handles them.
var documentExtensions = map[string]bool{
	// Office
	".docx": true, ".doc": true, ".odt": true,
	".xlsx": true, ".xls": true, ".ods": true,
	".pptx": true, ".ppt": true, ".odp": true,
	// E-books / publishing
	".epub": true,
	".tex":  true,
	// Web / markup / data
	".html": true, ".htm": true,
	".xml": true,
	".csv": true, ".tsv": true,
	".json": true, ".yaml": true, ".yml": true,
	// Notebooks / citations
	".ipynb": true,
	".bib":   true, ".ris": true,
	// Email
	".eml": true, ".msg": true,
	// Databases
	".dbf": true,
	// Rich text
	".rtf": true, ".rst": true,
	".txt": true,
}

// ScanDocuments walks a directory and returns all rich-document files. It does
// not extract text — that is the extractor backend's job (see internal/extractor).
// Like ScanImages/ScanPdfs, it hashes file contents so the indexer can skip
// unchanged files.
func ScanDocuments(dir string) ([]DocumentFile, error) {
	var files []DocumentFile
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !documentExtensions[ext] {
			return nil
		}

		// Skip files larger than 500MB to avoid memory pressure.
		if info.Size() > 500*1024*1024 {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files; matches ScanImages behavior
		}
		hash := sha256.Sum256(data)

		base := filepath.Base(path)
		name := strings.TrimSuffix(base, filepath.Ext(base))

		files = append(files, DocumentFile{
			Path:        path,
			Name:        name,
			ContentHash: fmt.Sprintf("%x", hash),
			Mtime:       float64(info.ModTime().Unix()),
		})
		return nil
	})
	return files, err
}
