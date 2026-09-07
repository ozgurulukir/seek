package source

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

type FileInfo struct {
	Path        string
	Title       string
	Content     string
	ContentHash string
	Mtime       float64
	LineCount   int
}

// ScanMarkdown scans a directory for markdown files matching the pattern.
func ScanMarkdown(dir, pattern string) ([]FileInfo, []ScanIssue, error) {
	if pattern == "" {
		pattern = "**/*.md"
	}

	var files []FileInfo
	var issues []ScanIssue

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			issues = append(issues, ScanIssue{Path: path, Err: err})
			return nil
		}
		if info.IsDir() {
			return nil
		}

		// Match markdown files
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".markdown" {
			return nil
		}

		// Check glob pattern (simple version: just check extension)
		if pattern != "**/*.md" && pattern != "**/*.markdown" {
			matched, _ := filepath.Match(pattern, filepath.Base(path))
			if !matched {
				return nil
			}
		}

		// Skip files larger than 50MB to avoid memory pressure.
		if info.Size() > markdownMaxFileSize {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, ScanIssue{Path: path, Err: err})
			return nil
		}
		content := string(data)

		hash := sha256.Sum256(data)
		title := extractMarkdownTitle(content, path)

		files = append(files, FileInfo{
			Path:        path,
			Title:       title,
			Content:     content,
			ContentHash: hex.EncodeToString(hash[:]),
			Mtime:       float64(info.ModTime().UnixNano()) / 1e9,
			LineCount:   strings.Count(content, "\n") + 1,
		})

		return nil
	})

	return files, issues, err
}

func extractMarkdownTitle(content, path string) string {
	lines := strings.SplitN(content, "\n", 20)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	// Fallback: use filename without extension
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
