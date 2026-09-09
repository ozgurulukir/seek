package source

import (
	"context"
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
	// Metadata holds structured frontmatter key/values (markdown) or derived
	// fields (code: lang, repo). Written to the fast_fields table so they are
	// filterable via FastFieldFilter without loading full documents.
	Metadata map[string]string
}

// ScanMarkdown scans a directory for markdown files matching the pattern.
func ScanMarkdown(dir, pattern string) ([]FileInfo, []ScanIssue, error) {
	return ScanMarkdownContext(context.Background(), dir, pattern)
}

// ScanMarkdownContext scans markdown files while honoring cancellation during
// both directory traversal and file reads.
func ScanMarkdownContext(ctx context.Context, dir, pattern string) ([]FileInfo, []ScanIssue, error) {
	if pattern == "" {
		pattern = "**/*.md"
	}

	var files []FileInfo
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
			Metadata:    parseFrontmatter(content),
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

// parseFrontmatter extracts YAML frontmatter from the top of a markdown
// document as a flat string map. Deliberately not a full YAML parser: only
// scalars (key: value), quoted strings, and inline/list tags are supported,
// which covers the metadata users put in notes. Keys are lowercased;
// unsupported shapes (nested maps, numbers) are stringified best-effort.
// The frontmatter text itself stays part of Content (indexed as before) —
// this is a metadata SSOT extraction, not a content policy change.
func parseFrontmatter(content string) map[string]string {
	meta := map[string]string{}
	lines := strings.SplitN(content, "\n", 512)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return meta
	}
	lastScalarKey := ""
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			break
		}
		// list item under the previous scalar key (e.g. "- tag" under "tags:")
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			if lastScalarKey != "" {
				item := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
				if meta[lastScalarKey] == "" {
					meta[lastScalarKey] = item
				} else {
					meta[lastScalarKey] = meta[lastScalarKey] + "," + item
				}
			}
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		value := strings.TrimSpace(line[idx+1:])
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		if value != "" {
			meta[key] = value
		}
		// A key with an empty value ("tags:") becomes the container that
		// subsequent "- item" list lines attach to.
		lastScalarKey = key
	}
	return meta
}
