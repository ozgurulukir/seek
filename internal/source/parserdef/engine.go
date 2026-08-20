package parserdef

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Match returns the first source + version that matches the current environment,
// plus the discovered source files. This is the detect entry point used by the
// indexer and CLI.
func (d *ParserDef) Match() (src *SourceSpec, ver *VersionSpec, files []string, err error) {
	for i := range d.Sources {
		s := &d.Sources[i]
		switch s.Driver {
		case "sqlite":
			return detectSQLiteSource(d)
		case "jsonl", "jsonfiles":
			return detectJSONLSource(d)
		}
	}
	return nil, nil, nil, fmt.Errorf("parser %q: no supported driver found in sources", d.Name)
}

// SyncSessions reads all sessions from matched sources, optionally filtering by
// a cursor threshold. When since is the zero time, all sessions are returned with
// their messages (full index/reindex).
//
// In incremental mode (since non-zero), ALL sessions are returned (so the caller
// can detect orphans), but sessions whose cursor hasn't advanced past `since` are
// returned with Messages=nil (unchanged — the caller should skip re-indexing them).
// This avoids the need for a separate "list all IDs" query.
func SyncSessions(src *SourceSpec, ver *VersionSpec, files []string, since time.Time) ([]Session, []SessionError, error) {
	switch src.Driver {
	case "sqlite":
		return syncSQLiteSessions(src, ver, files, since)
	case "jsonl", "jsonfiles":
		return syncJSONLSessions(src, ver, files, since)
	default:
		return nil, nil, fmt.Errorf("driver %q not yet supported", src.Driver)
	}
}

// syncJSONLSessions reads all JSONL files, producing one Session per file.
// Incremental sync uses file mtime as cursor.
func syncJSONLSessions(src *SourceSpec, ver *VersionSpec, files []string, since time.Time) ([]Session, []SessionError, error) {
	var sessions []Session
	var errs []SessionError

	for _, filePath := range files {
		row, err := scanJSONLFile(filePath, ver)
		if err != nil {
			errs = append(errs, SessionError{SessionID: filePath, Err: fmt.Errorf("scan jsonl: %w", err)})
			continue
		}

		// Determine cursor.
		var cursor time.Time
		if ver.Sessions.CursorFromMtime {
			cursor = row.mtime
		} else if ver.Sessions.Cursor != "" {
			ct, cerr := normalizeCursor(row.cursor, ver.Sessions.CursorFormat)
			if cerr != nil {
				errs = append(errs, SessionError{SessionID: row.id, Err: fmt.Errorf("cursor: %w", cerr)})
				continue
			}
			cursor = ct
		}

		sess := Session{
			ID:       row.id,
			SrcPath:  filePath,
			Cursor:   cursor,
			Metadata: row.metadata,
		}

		// Incremental filter: skip unchanged sessions (Messages=nil for unchanged).
		if !since.IsZero() && !cursor.IsZero() && !cursor.After(since) {
			sessions = append(sessions, sess) // Messages stays nil → unchanged
			continue
		}

		sess.Messages = row.messages
		sessions = append(sessions, sess)
	}

	return sessions, errs, nil
}

// syncSQLiteSessions reads all sessions from matched SQLite databases,
// optionally filtering by a cursor threshold.
func syncSQLiteSessions(src *SourceSpec, ver *VersionSpec, files []string, since time.Time) ([]Session, []SessionError, error) {
	var sessions []Session
	var errs []SessionError

	for _, dbPath := range files {
		db, err := openExternalDB(dbPath)
		if err != nil {
			errs = append(errs, SessionError{SessionID: dbPath, Err: fmt.Errorf("open db: %w", err)})
			continue
		}

		rawRows, err := scanSQLiteSessions(db, ver)
		if err != nil {
			db.Close()
			errs = append(errs, SessionError{SessionID: dbPath, Err: fmt.Errorf("scan sessions: %w", err)})
			continue
		}

		var batchIDs []string
		var batchIndices []int

		for _, raw := range rawRows {
			// Normalize cursor.
			var cursor time.Time
			if ver.Sessions.Cursor != "" {
				ct, cerr := normalizeCursor(raw.cursor, ver.Sessions.CursorFormat)
				if cerr != nil {
					errs = append(errs, SessionError{SessionID: raw.id, Err: fmt.Errorf("cursor: %w", cerr)})
					continue
				}
				cursor = ct
			}

			// Determine if this session has changed (needs re-indexing).
			// In incremental mode, unchanged sessions get Messages=nil so the
			// caller skips them but can still track them for orphan detection.
			sess := Session{
				ID:       raw.id,
				Title:    raw.title,
				SrcPath:  dbPath,
				Cursor:   cursor,
				Metadata: raw.metadata,
			}

			// Incremental filter: skip message fetch for unchanged sessions.
			// A zero cursor (unparseable / missing) is treated as "always re-index".
			if !since.IsZero() && cursor.IsZero() == false && !cursor.After(since) {
				sessions = append(sessions, sess) // Messages stays nil → unchanged
				continue
			}

			// Fetch messages (Mode A: query, or Mode B: inline).
			if ver.Messages.Inline {
				msgs, mErr := parseInlineMessages(raw.data, &ver.Messages)
				if mErr != nil {
					errs = append(errs, SessionError{SessionID: raw.id, Err: fmt.Errorf("inline messages: %w", mErr)})
					continue
				}
				sess.Messages = msgs
				sessions = append(sessions, sess)
			} else {
				idx := len(sessions)
				sessions = append(sessions, sess)
				batchIDs = append(batchIDs, raw.id)
				batchIndices = append(batchIndices, idx)

				if len(batchIDs) >= 500 {
					errs = append(errs, fetchAndAssignBatch(db, ver, batchIDs, batchIndices, sessions)...)
					batchIDs = batchIDs[:0]
					batchIndices = batchIndices[:0]
				}
			}
		}

		if len(batchIDs) > 0 {
			errs = append(errs, fetchAndAssignBatch(db, ver, batchIDs, batchIndices, sessions)...)
		}

		db.Close()
	}

	return sessions, errs, nil
}

// parseInlineMessages decodes and extracts messages from an inline blob (Mode B, Zed).
func parseInlineMessages(data []byte, spec *MessagesSpec) ([]Message, error) {
	if len(data) == 0 {
		return nil, nil
	}

	decoded := data
	// Optional blob decode.
	if spec.Decode == "zstd" {
		dec, err := zstd.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("create zstd reader: %w", err)
		}
		defer dec.Close()
		decoded, err = dec.DecodeAll(data, nil)
		if err != nil {
			return nil, fmt.Errorf("zstd decode: %w", err)
		}
	}

	// Parse the top-level JSON.
	var top interface{}
	if err := json.Unmarshal(decoded, &top); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	// Navigate to the items array (spec.Items is a dot-path, e.g. "messages").
	itemsRaw := navigateJSON(top, spec.Items)
	arr, ok := itemsRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("items path %q is not an array", spec.Items)
	}

	var messages []Message
	for _, elem := range arr {
		obj, ok := elem.(map[string]interface{})
		if !ok {
			continue
		}
		// Find first matching variant.
		for _, v := range spec.Variants {
			if _, hasKey := obj[v.MatchKey]; !hasKey {
				continue
			}
			// Extract text from the variant.
			text := extractVariantText(obj[v.MatchKey], v)
			if text == "" {
				continue
			}
			messages = append(messages, Message{Role: v.Role, Content: text})
			break // first matching variant wins
		}
	}
	return messages, nil
}

// navigateJSON traverses a dot-path (e.g. "a.b.c") through nested JSON maps.
func navigateJSON(v interface{}, path string) interface{} {
	if path == "" {
		return v
	}
	parts := splitDotPath(path)
	cur := v
	for _, p := range parts {
		obj, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = obj[p]
	}
	return cur
}

// splitDotPath splits "a.b.c" into ["a","b","c"].
func splitDotPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			parts = append(parts, path[start:i])
			start = i + 1
		}
	}
	parts = append(parts, path[start:])
	return parts
}

// extractVariantText extracts concatenated text from a message element.
// The element is the value of the match_key (e.g. the "User" or "Agent" object).
// It looks up array_field within the element, then scans each array member's
// text_keys (dot-path supported) and concatenates the first found string values.
func extractVariantText(element interface{}, v VariantSpec) string {
	obj, ok := element.(map[string]interface{})
	if !ok {
		return ""
	}
	arrRaw := obj[v.ArrayField]
	arr, ok := arrRaw.([]interface{})
	if !ok {
		// If array_field is not an array but a direct field on the element,
		// try text_keys directly on the element itself.
		for _, key := range v.TextKeys {
			if s := getStringFromPath(obj, key); s != "" {
				return s
			}
		}
		return ""
	}

	var parts []string
	for _, item := range arr {
		itemObj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		for _, key := range v.TextKeys {
			if s := getStringFromPath(itemObj, key); s != "" {
				parts = append(parts, s)
				break // first found key in this element wins
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return joinStrings(parts, "\n")
}

// getStringFromPath gets a string value at a dot-path from a JSON object map.
// Returns "" if the path doesn't exist or the value isn't a string.
func getStringFromPath(obj map[string]interface{}, path string) string {
	parts := splitDotPath(path)
	cur := interface{}(obj)
	for _, p := range parts {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return ""
		}
		cur = m[p]
	}
	s, ok := cur.(string)
	if !ok {
		return ""
	}
	return s
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}
