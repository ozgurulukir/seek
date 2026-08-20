package source

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PdfFile represents a PDF found on disk (before page rasterization).
type PdfFile struct {
	Path        string
	Name        string // filename without extension
	ContentHash string
	Mtime       float64
}

// ScanPdfs walks a directory and returns all PDF files. Page rasterization and
// text extraction now live in the extraction domain (internal/extractor/builtin);
// this scanner only discovers files and hashes their contents so the indexer
// can skip unchanged PDFs.
func ScanPdfs(dir string) ([]PdfFile, error) {
	var files []PdfFile
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".pdf" {
			return nil
		}
		// Skip files larger than 500MB to avoid memory pressure.
		if info.Size() > 500*1024*1024 {
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
