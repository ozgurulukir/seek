package parserdef

import (
	"database/sql"
	"encoding/json"
	"regexp"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// expandTilde expands a leading ~ to the user's home directory.
func expandTilde(p string) string {
	if p == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// matchGlob expands glob patterns, applying exclude filters.
func matchGlob(patterns, excludes []string) ([]string, error) {
	var result []string
	seen := make(map[string]bool)
	for _, pat := range patterns {
		expanded := expandTilde(pat)
		matches, err := filepath.Glob(expanded)
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", pat, err)
		}
		for _, m := range matches {
			if seen[m] {
				continue
			}
			if isExcluded(m, excludes) {
				continue
			}
			seen[m] = true
			result = append(result, m)
		}
	}
	return result, nil
}

func isExcluded(path string, excludes []string) bool {
	base := filepath.Base(path)
	for _, ex := range excludes {
		matched, _ := filepath.Match(ex, base)
		if matched {
			return true
		}
		// Also match against full path (exclude patterns may include dirs).
		matched, _ = filepath.Match(ex, path)
		if matched {
			return true
		}
	}
	return false
}

// --- Detection ---

// evalWhen evaluates a WhenRule against the filesystem and (optionally) an external DB.
// If db is nil, only filesystem rules (file_exists, dir_exists) are evaluated;
// table/column rules return false (they require an open DB connection).
func evalWhen(rule *WhenRule, db *sql.DB) bool {
	if rule == nil {
		return true // no rule → always matches
	}
	if rule.FileExists != "" {
		if _, err := os.Stat(expandTilde(rule.FileExists)); err != nil {
			return false
		}
	}
	if rule.DirExists != "" {
		info, err := os.Stat(expandTilde(rule.DirExists))
		if err != nil || !info.IsDir() {
			return false
		}
	}
	if rule.TableExists != "" {
		if db == nil {
			return false
		}
		if !tableExists(db, rule.TableExists) {
			return false
		}
	}
	if rule.ColumnExists[0] != "" {
		if db == nil {
			return false
		}
		if !columnExists(db, rule.ColumnExists[0], rule.ColumnExists[1]) {
			return false
		}
	}
	return true
}

func tableExists(db *sql.DB, table string) bool {
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
	).Scan(&name)
	return err == nil
}

// isSafeIdentifier checks that a string is a plausible SQL identifier
// (alphanumeric + underscore). Used to guard PRAGMA table_info interpolation.
func isSafeIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '_' && !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func columnExists(db *sql.DB, table, column string) bool {
	// PRAGMA can't bind the table name, so we validate it's a sane identifier
	// (letters, digits, underscore only) before interpolation.
	if !isSafeIdentifier(table) {
		return false
	}
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

// --- Cursor normalization ---

func normalizeCursor(raw string, format string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	switch format {
	case "epoch_ms":
		var ms int64
		if _, err := fmt.Sscanf(raw, "%d", &ms); err != nil {
			return time.Time{}, fmt.Errorf("epoch_ms cursor %q: %w", raw, err)
		}
		return time.UnixMilli(ms), nil
	case "epoch_s":
		var s int64
		if _, err := fmt.Sscanf(raw, "%d", &s); err != nil {
			return time.Time{}, fmt.Errorf("epoch_s cursor %q: %w", raw, err)
		}
		return time.Unix(s, 0), nil
	case "rfc3339":
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, fmt.Errorf("rfc3339 cursor %q: %w", raw, err)
		}
		return t, nil
	case "datetime":
		t, err := time.Parse("2006-01-02 15:04:05", raw)
		if err != nil {
			return time.Time{}, fmt.Errorf("datetime cursor %q: %w", raw, err)
		}
		return t, nil
	default:
		return time.Time{}, fmt.Errorf("unknown cursor_format %q", format)
	}
}

// --- SQLite driver ---

// sqliteSessionRow is a raw session row from the external DB.
type sqliteSessionRow struct {
	id       string
	title    string
	cursor   string
	data     []byte // inline blob (may be nil)
	metadata map[string]string
}

// openExternalDB opens an external SQLite DB in read-only mode with a busy timeout.
// We do not set _journal_mode: read-only connections cannot change it, and the
// source DB already has its own journal mode. busy_timeout lets us wait if the
// source app is mid-write (WAL readers don't block, but this guards edge cases).
func openExternalDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=2000", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open external db: %w", err)
	}
	return db, nil
}

// detectSQLiteSource returns the first matching source and version for the given ParserDef,
// opening each discovered DB to test version-level detect rules.
func detectSQLiteSource(def *ParserDef) (*SourceSpec, *VersionSpec, []string, error) {
	for si := range def.Sources {
		src := &def.Sources[si]
		if src.Driver != "sqlite" {
			continue
		}
		// Source-level filesystem check.
		if !evalWhen(src.When, nil) {
			continue
		}
		// Discover candidate DB files.
		files, err := matchGlob(src.Paths, src.Exclude)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(files) == 0 {
			continue
		}
		// Try version detection against the first openable DB.
		// Per the plan: "version detect first DB'ye göre yapılır."
		var matchedVer *VersionSpec
		for _, f := range files {
			db, err := openExternalDB(f)
			if err != nil {
				continue
			}
			for vi := range src.Versions {
				ver := &src.Versions[vi]
				if evalWhen(ver.When, db) {
					matchedVer = ver
					break
				}
			}
			db.Close()
			if matchedVer != nil {
				return src, matchedVer, files, nil
			}
		}
		// If no version matched in any DB but the source itself matched,
		// and there is at least one version with no When rule, use it.
		if matchedVer == nil {
			for vi := range src.Versions {
				ver := &src.Versions[vi]
				if ver.When == nil {
					return src, ver, files, nil
				}
			}
		}
	}
	return nil, nil, nil, fmt.Errorf("parser %q: no matching source/version found (checked %d sources)",
		def.Name, len(def.Sources))
}

// scanSQLiteSessions runs the sessions query against a DB file and returns raw rows.
var (
	reSessionBind = regexp.MustCompile(`(?i)([\w.]+)\s*=\s*:session_id`)
	reSelect      = regexp.MustCompile(`(?i)\bSELECT\b`)
)

// buildBatchQuery rewrites a query designed for a single session
// to one that queries multiple sessions using json_each.
func buildBatchQuery(query string) (string, int, error) {
	match := reSessionBind.FindStringSubmatch(query)
	if len(match) < 2 {
		return "", 0, fmt.Errorf("could not find \"= :session_id\" in query")
	}
	col := match[1]

	q := reSelect.ReplaceAllStringFunc(query, func(s string) string {
		return s + " " + col + " AS _session_id,"
	})

	count := 0
	q = reSessionBind.ReplaceAllStringFunc(q, func(s string) string {
		count++
		return fmt.Sprintf("%s IN (SELECT value FROM json_each(?))", col)
	})

	return q, count, nil
}

func scanSQLiteSessions(db *sql.DB, ver *VersionSpec) ([]sqliteSessionRow, error) {
	rows, err := db.Query(ver.Sessions.Query)
	if err != nil {
		return nil, fmt.Errorf("sessions query: %w", err)
	}
	defer rows.Close()

	// Determine column indices for configured field names.
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("sessions columns: %w", err)
	}
	colIdx := make(map[string]int)
	for i, c := range cols {
		colIdx[c] = i
	}

	idIdx := colIdx[ver.Sessions.ID]
	titleIdx := -1
	if ver.Sessions.Title != "" {
		titleIdx = colIdx[ver.Sessions.Title]
	}
	cursorIdx := -1
	if ver.Sessions.Cursor != "" {
		cursorIdx = colIdx[ver.Sessions.Cursor]
	}
	dataIdx := -1
	if ver.Sessions.Data != "" {
		dataIdx = colIdx[ver.Sessions.Data]
	}
	// Metadata column indices.
	metaIdx := make(map[string]int) // common field name → column index
	for field, col := range ver.Sessions.Metadata {
		if idx, ok := colIdx[col]; ok {
			metaIdx[field] = idx
		}
	}

	var result []sqliteSessionRow
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan session row: %w", err)
		}

		row := sqliteSessionRow{metadata: make(map[string]string)}
		if idIdx >= 0 {
			row.id = vals[idIdx].String
		}
		if titleIdx >= 0 {
			row.title = vals[titleIdx].String
		}
		if cursorIdx >= 0 {
			row.cursor = vals[cursorIdx].String
		}
		if dataIdx >= 0 {
			row.data = []byte(vals[dataIdx].String)
		}
		for field, idx := range metaIdx {
			row.metadata[field] = vals[idx].String
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// fetchSQLiteMessages runs the messages query for a single session (Mode A).

// fetchSQLiteMessagesBatch runs the messages query for multiple sessions.
func fetchSQLiteMessagesBatch(db *sql.DB, ver *VersionSpec, sessionIDs []string) (map[string][]Message, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}

	q, bindCount, err := buildBatchQuery(ver.Messages.Query)
	if err != nil {
		return nil, fmt.Errorf("build batch query: %w", err)
	}

	idsJSON, err := json.Marshal(sessionIDs)
	if err != nil {
		return nil, fmt.Errorf("marshal session ids: %w", err)
	}

	args := make([]interface{}, bindCount)
	for i := 0; i < bindCount; i++ {
		args[i] = string(idsJSON)
	}

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("batch messages query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("messages columns: %w", err)
	}
	colIdx := make(map[string]int)
	for i, c := range cols {
		colIdx[c] = i
	}
	sessionIdx := colIdx["_session_id"]
	roleIdx := colIdx[ver.Messages.Role]
	contentIdx := colIdx[ver.Messages.Content]

	result := make(map[string][]Message)

	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan message row: %w", err)
		}
		sessionID := ""
		role := ""
		content := ""
		if sessionIdx >= 0 {
			sessionID = vals[sessionIdx].String
		}
		if roleIdx >= 0 {
			role = vals[roleIdx].String
		}
		if contentIdx >= 0 {
			content = vals[contentIdx].String
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		result[sessionID] = append(result[sessionID], Message{Role: role, Content: content})
	}
	return result, rows.Err()
}
