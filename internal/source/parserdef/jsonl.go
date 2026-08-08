package parserdef

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- JSONL driver ---
//
// The jsonl driver reads conversation data from JSONL files (one JSON object per line).
// Each file represents one session (Claude, Codex) or one line is one message (Gemini-style).
// The driver handles three challenges that the sqlite driver doesn't face:
//
//  1. Multi-line aggregation: a session spans many JSONL lines, and different line
//     types carry different data (e.g. Codex `session_meta` line carries the session ID,
//     `response_item` lines carry messages).
//  2. Per-line type filtering: only lines whose top-level `type` field matches
//     `messages.line_filter` are processed as messages.
//  3. Asymmetric content paths: Claude user messages store content at `message.content`
//     while assistant messages store it directly at `message`. This is handled by
//     `content_path` (default) and `content_path_user` (override for user role).

// jsonlSessionRow is a parsed session from a JSONL file.
type jsonlSessionRow struct {
	id       string
	cursor   string // raw cursor string (empty if cursor_from_mtime)
	mtime    time.Time
	messages []Message
	metadata map[string]string
}

// detectJSONLSource discovers JSONL files matching the source spec.
func detectJSONLSource(def *ParserDef) (*SourceSpec, *VersionSpec, []string, error) {
	for si := range def.Sources {
		src := &def.Sources[si]
		if src.Driver != "jsonl" && src.Driver != "jsonfiles" {
			continue
		}
		// Source-level filesystem check.
		if !evalWhen(src.When, nil) {
			continue
		}
		// Discover JSONL files by recursive walk.
		files, err := walkJSONLFiles(src.Paths, src.Exclude)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(files) == 0 {
			continue
		}
		// Version detection: jsonl/jsonfiles has no DB to inspect, so pick
		// the first version (there's usually only one).
		for vi := range src.Versions {
			return src, &src.Versions[vi], files, nil
		}
	}
	return nil, nil, nil, fmt.Errorf("parser %q: no matching source/version found (checked %d sources)",
		def.Name, len(def.Sources))
}

// walkJSONLFiles recursively walks the given directories and returns all .jsonl files,
// applying exclude filters. This mirrors the native ScanClaudeFiles/ScanCodexFiles behavior.
func walkJSONLFiles(paths, excludes []string) ([]string, error) {
	var result []string
	seen := make(map[string]bool)

	for _, p := range paths {
		root := expandTilde(p)
		info, err := os.Stat(root)
		if err != nil {
			continue // path doesn't exist, skip
		}
		if !info.IsDir() {
			// A single file path — include if it's a .jsonl (or any file for jsonfiles driver).
			if !seen[root] && !isExcluded(root, excludes) {
				result = append(result, root)
				seen[root] = true
			}
			continue
		}
		// Walk the directory tree. Per-file traversal errors are skipped (the
		// walk continues to other subtrees). This matches the native parser
		// pattern (ScanClaudeFiles/ScanCodexFiles).
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // skip unreadable paths, continue walking
			}
			if info.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".jsonl" {
				return nil
			}
			if isExcluded(path, excludes) {
				return nil
			}
			if !seen[path] {
				result = append(result, path)
				seen[path] = true
			}
			return nil
		})
	}
	return result, nil
}

// scanJSONLFile reads a single JSONL file and produces a session row.
func scanJSONLFile(filePath string, ver *VersionSpec) (*jsonlSessionRow, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}

	row := &jsonlSessionRow{
		mtime:    stat.ModTime(),
		metadata: make(map[string]string),
	}

	// Session ID from filename.
	if ver.Sessions.IDFromFilename {
		row.id = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	filterSet := make(map[string]bool)
	for _, t := range ver.Messages.LineFilter {
		filterSet[t] = true
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Parse the top-level structure to get the type discriminator.
		var topLevel struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &topLevel); err != nil {
			continue // malformed JSON line, skip
		}

		// Check if this line type is a message line.
		if filterSet[topLevel.Type] {
			msg, ok := parseJSONLMessageLine(line, topLevel.Type, ver)
			if ok {
				row.messages = append(row.messages, msg)
			}
			// Extract metadata from message lines (cwd, etc. are on user/assistant lines).
			extractJSONLMetadata(line, ver, row.metadata)
			continue
		}

		// Non-message line: may carry session metadata (e.g. Codex session_meta for ID).
		if ver.Sessions.ID != "" && !ver.Sessions.IDFromFilename {
			// Try to extract session ID from the field path if this line has it.
			if id := extractJSONFieldString(line, ver.Sessions.ID); id != "" && row.id == "" {
				row.id = id
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	// If ID still empty and not from filename, fall back to filename.
	if row.id == "" {
		row.id = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	}

	return row, nil
}

// parseJSONLMessageLine extracts a single message from a JSONL line that passed the type filter.
func parseJSONLMessageLine(line, lineType string, ver *VersionSpec) (Message, bool) {
	// Determine role from role_field path.
	role := extractJSONFieldString(line, ver.Messages.RoleField)
	if role == "" {
		return Message{}, false
	}

	// Only user/assistant roles are indexed (matches native parser behavior).
	// Other roles (system, etc.) are silently dropped.
	if role != "user" && role != "assistant" {
		return Message{}, false
	}

	// Determine content path: user role may use a different path (Claude asymmetry).
	// Paths may be comma-separated fallbacks (e.g. "message.content,message" to try
	// the modern wrapper-dict format first, then the legacy direct format).
	contentPaths := ver.Messages.ContentPath
	if role == "user" && ver.Messages.ContentPathUser != "" {
		contentPaths = ver.Messages.ContentPathUser
	}

	content := extractJSONLContentFromPaths(line, contentPaths, ver.Messages.TextTypes)
	if content == "" {
		return Message{}, false
	}

	return Message{Role: role, Content: content}, true
}

// extractJSONFieldString navigates a dot-path in a JSON line and returns the string value.
func extractJSONFieldString(line, path string) string {
	if path == "" {
		return ""
	}
	var obj interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return ""
	}
	val := navigateJSON(obj, path)
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}

// extractJSONLContentFromPaths navigates to content within a JSON line using
// comma-separated fallback paths (first non-empty result wins), then extracts
// text from the value (string, or array of {type, text} blocks).
func extractJSONLContentFromPaths(line, paths string, textTypes []string) string {
	var obj interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return ""
	}
	for _, path := range strings.Split(paths, ",") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		val := navigateJSON(obj, path)
		if content := extractContentValue(val, textTypes); content != "" {
			return content
		}
	}
	return ""
}

// extractContentValue extracts text from a JSON value that may be:
// - a plain string → return directly
// - an array of {type, text} blocks → filter by textTypes (empty = accept all), join with "\n"
func extractContentValue(val interface{}, textTypes []string) string {
	// Try as string.
	if s, ok := val.(string); ok {
		return s
	}

	// Try as array of content blocks.
	arr, ok := val.([]interface{})
	if !ok {
		return ""
	}

	// Build type whitelist. Empty textTypes = accept all block types.
	typeSet := make(map[string]bool)
	hasFilter := len(textTypes) > 0
	for _, t := range textTypes {
		typeSet[t] = true
	}

	var parts []string
	for _, elem := range arr {
		block, ok := elem.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		if hasFilter && !typeSet[blockType] {
			continue
		}
		text, _ := block["text"].(string)
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// extractJSONLMetadata reads metadata field paths from a JSONL line and stores
// the first non-empty value found for each known metadata field.
func extractJSONLMetadata(line string, ver *VersionSpec, metadata map[string]string) {
	for field, path := range ver.Sessions.Metadata {
		if metadata[field] != "" {
			continue // already found
		}
		val := extractJSONFieldString(line, path)
		if val != "" {
			metadata[field] = val
		}
	}
}
