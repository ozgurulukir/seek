package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"

	TitleMaxLen        = 100
	ImageContextMaxLen = 500

	markdownMaxFileSize = 50 * 1024 * 1024
	documentMaxFileSize = 500 * 1024 * 1024
)

// ScanIssue identifies an I/O failure encountered while scanning a collection.
// The scan continues so healthy files can still be indexed.
type ScanIssue struct {
	Path string
	Err  error
}

// ConversationImage represents an image extracted from a conversation.
type ConversationImage struct {
	Data      []byte // decoded base64
	MediaType string // "image/png", "image/jpeg"
	Context   string // surrounding text from the conversation
	Index     int    // position in conversation
	SavedPath string // path where image was saved to disk
}

// SaveImage writes image data to the given path, creating directories as needed.
func SaveImage(data []byte, mediaType string, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), config.DefaultDirPerms); err != nil {
		return fmt.Errorf("create image dir: %w", err)
	}
	return os.WriteFile(path, data, config.DefaultFilePerms)
}

// ExtractExtension returns the file extension for a given media type.
func ExtractExtension(mediaType string) string {
	switch strings.ToLower(mediaType) {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/svg+xml":
		return "svg"
	default:
		return "bin"
	}
}

// hashFileContext returns the SHA-256 hex digest of a file's bytes. The
// Claude and Codex scanners use it so the incremental skip compares content,
// not just mtime — an in-place edit that preserves the timestamp must still
// trigger a re-index.
func hashFileContext(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Stream instead of os.ReadFile: conversation histories can be large and
	// this runs on every scan, so peak memory must not scale with file size.
	hash := sha256.New()
	buf := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, err := hash.Write(buf[:n]); err != nil {
				return "", err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ImageCacheDir returns the path to the image cache directory.
func ImageCacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "seek", "images")
}
