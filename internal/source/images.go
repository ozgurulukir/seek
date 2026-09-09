package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// ImageFile represents a single image file found on disk.
type ImageFile struct {
	Path        string
	Name        string // filename without extension
	ContentHash string
	Mtime       float64
}

// imageExtensions are the file extensions we index.
var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true,
	".webp": true, ".gif": true, ".bmp": true,
	".tiff": true, ".tif": true, ".svg": true,
}

// ScanImages walks a directory and returns all image files plus any scan
// issues encountered (walk or read errors). I/O failures are surfaced as
// ScanIssue entries so the indexer can log them; the scan continues so
// healthy files are still indexed.
func ScanImages(dir string) ([]ImageFile, []ScanIssue, error) {
	return ScanImagesContext(context.Background(), dir)
}

func ScanImagesContext(ctx context.Context, dir string) ([]ImageFile, []ScanIssue, error) {
	var files []ImageFile
	var issues []ScanIssue
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			issues = append(issues, ScanIssue{Path: path, Err: err})
			return nil
		}
		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !imageExtensions[ext] {
			return nil
		}

		// Skip files larger than 100MB to avoid memory pressure.
		if info.Size() > 100*1024*1024 {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, ScanIssue{Path: path, Err: err})
			return nil
		}
		hash := sha256.Sum256(data)

		base := filepath.Base(path)
		name := strings.TrimSuffix(base, filepath.Ext(base))

		files = append(files, ImageFile{
			Path:        path,
			Name:        name,
			ContentHash: hex.EncodeToString(hash[:]),
			Mtime:       float64(info.ModTime().UnixNano()) / 1e9,
		})
		return nil
	})

	return files, issues, err
}
