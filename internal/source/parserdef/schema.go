// Package parserdef implements a schema-driven parser engine for indexing
// conversation sources (SQLite, JSONL, JSON files) via declarative YAML schemas.
// The engine is fixed Go code; per-platform knowledge lives in schema files.
package parserdef

import (
	"fmt"
	"strings"
	"time"
)

// ParserDef is the top-level schema type. One per platform.
type ParserDef struct {
	Format      int          `yaml:"format"` // schema file format version (must be 1)
	Name        string       `yaml:"name"`   // parser name; matches filename and collection parser_name
	Description string       `yaml:"description"`
	Sources     []SourceSpec `yaml:"sources"`
}

// SourceSpec describes one discovery source (ordered; first matching When wins).
type SourceSpec struct {
	Driver   string        `yaml:"driver"` // sqlite | jsonl | jsonfiles
	When     *WhenRule     `yaml:"when"`
	Paths    []string      `yaml:"paths"`
	Exclude  []string      `yaml:"exclude"`
	Versions []VersionSpec `yaml:"versions"`
}

// WhenRule is a detect rule. Any set field is tested; all set fields must pass.
type WhenRule struct {
	FileExists   string    `yaml:"file_exists"`
	DirExists    string    `yaml:"dir_exists"`
	TableExists  string    `yaml:"table_exists"`
	ColumnExists [2]string `yaml:"column_exists"` // [table, column]
}

// VersionSpec describes one schema version of a source (ordered; first matching When wins).
type VersionSpec struct {
	Version  int          `yaml:"version"`
	When     *WhenRule    `yaml:"when"` // version-level detect (table/column checks)
	Sessions SessionsSpec `yaml:"sessions"`
	Messages MessagesSpec `yaml:"messages"`
}

// SessionsSpec describes how to query sessions from the source.
type SessionsSpec struct {
	Query           string            `yaml:"query"`
	ID              string            `yaml:"id"`
	Title           string            `yaml:"title"`
	Cursor          string            `yaml:"cursor"`
	CursorFormat    string            `yaml:"cursor_format"`     // epoch_ms | epoch_s | rfc3339 | datetime
	Data            string            `yaml:"data"`              // inline mode: column carrying full session blob
	Metadata        map[string]string `yaml:"metadata"`          // common field name → SQL column
	IDFromFilename  bool              `yaml:"id_from_filename"`  // jsonl: ID = filename sans extension
	CursorFromMtime bool              `yaml:"cursor_from_mtime"` // jsonl: cursor = file mtime
}

// MessagesSpec describes how to extract messages.
// Mode A (query): a SQL query with :session_id bind param returning role/content rows.
// Mode B (inline): messages parsed from the session row's data blob.
// Mode C (jsonl): line-by-line parsing of a JSONL file with type-based filtering.
type MessagesSpec struct {
	Query    string        `yaml:"query"`
	Role     string        `yaml:"role"`
	Content  string        `yaml:"content"`
	Inline   bool          `yaml:"inline"`
	Decode   string        `yaml:"decode"` // "" | zstd
	Items    string        `yaml:"items"`  // inline: JSON key for the message array
	Variants []VariantSpec `yaml:"variants"`

	// JSONL driver fields (Mode C).
	LineFilter      []string `yaml:"line_filter"`       // top-level `type` values that are message lines
	RoleField       string   `yaml:"role_field"`        // dot-path to the role value within a matching line
	ContentPath     string   `yaml:"content_path"`      // dot-path to content for assistant/unknown role
	ContentPathUser string   `yaml:"content_path_user"` // dot-path to content for user role (Claude asymmetry)
	TextTypes       []string `yaml:"text_types"`        // block type whitelist for array content (text, input_text, output_text)
}

// VariantSpec is a first-match rule for inline message elements.
type VariantSpec struct {
	MatchKey   string   `yaml:"match_key"`
	Role       string   `yaml:"role"`
	ArrayField string   `yaml:"array_field"`
	TextKeys   []string `yaml:"text_keys"`
}

// Session is the output model consumed by the indexer.
type Session struct {
	ID       string
	Title    string
	SrcPath  string            // source DB/file path (document.path prefix)
	Cursor   time.Time         // normalized cursor
	Metadata map[string]string // common-terminology values (workspace, parent, ...)
	Messages []Message
}

// Message is a single conversation message.
type Message struct {
	Role    string // source.RoleUser / RoleAssistant
	Content string
}

// SessionError carries a non-fatal warning for a session that was skipped.
type SessionError struct {
	SessionID string
	Err       error
}

// knownDrivers are the supported source drivers.
var knownDrivers = map[string]bool{
	"sqlite":    true,
	"jsonl":     true,
	"jsonfiles": true,
}

// knownCursorFormats are the supported cursor_format values.
var knownCursorFormats = map[string]bool{
	"epoch_ms": true,
	"epoch_s":  true,
	"rfc3339":  true,
	"datetime": true,
}

// knownMetadataFields is the fixed vocabulary of common metadata field names.
// New fields require a plan change (SSOT).
var knownMetadataFields = map[string]bool{
	"workspace": true,
	"parent":    true,
}

// Validate checks the schema for required fields, enum values, and SELECT-only queries.
func (d *ParserDef) Validate() error {
	if d.Format != 1 {
		return fmt.Errorf("schema %q: unsupported format %d (expected 1)", d.Name, d.Format)
	}
	if d.Name == "" {
		return fmt.Errorf("schema: name is required")
	}
	if len(d.Sources) == 0 {
		return fmt.Errorf("schema %q: at least one source is required", d.Name)
	}
	for i := range d.Sources {
		if err := d.Sources[i].validate(d.Name, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *SourceSpec) validate(parserName string, srcIdx int) error {
	prefix := fmt.Sprintf("schema %q source[%d]", parserName, srcIdx)
	if !knownDrivers[s.Driver] {
		return fmt.Errorf("%s: unknown driver %q (want sqlite|jsonl|jsonfiles)", prefix, s.Driver)
	}
	if len(s.Paths) == 0 {
		return fmt.Errorf("%s: paths is required", prefix)
	}
	if len(s.Versions) == 0 {
		return fmt.Errorf("%s: at least one version is required", prefix)
	}
	for i := range s.Versions {
		if err := s.Versions[i].validate(prefix, i, s.Driver); err != nil {
			return err
		}
	}
	return nil
}

func (v *VersionSpec) validate(srcPrefix string, verIdx int, driver string) error {
	prefix := fmt.Sprintf("%s version[%d]", srcPrefix, verIdx)
	if v.Version <= 0 {
		return fmt.Errorf("%s: version must be a positive integer", prefix)
	}
	// Cursor format check (if a cursor column or mtime cursor is specified).
	if v.Sessions.Cursor != "" || v.Sessions.CursorFromMtime {
		cf := v.Sessions.CursorFormat
		if cf == "" {
			return fmt.Errorf("%s: cursor_format is required when cursor or cursor_from_mtime is set", prefix)
		}
		if !knownCursorFormats[cf] {
			return fmt.Errorf("%s: unknown cursor_format %q (want epoch_ms|epoch_s|rfc3339|datetime)", prefix, cf)
		}
	}
	// Metadata field names must be in the known vocabulary.
	for field := range v.Sessions.Metadata {
		if !knownMetadataFields[field] {
			return fmt.Errorf("%s: unknown metadata field %q (known: workspace, parent)", prefix, field)
		}
	}
	// Sessions query must be SELECT-only (for sqlite driver).
	if v.Sessions.Query != "" {
		if err := validateSelectOnly(prefix+".sessions.query", v.Sessions.Query); err != nil {
			return err
		}
	}
	// Messages mode validation depends on driver.
	if driver == "jsonl" || driver == "jsonfiles" {
		// JSONL driver: line_filter + role_field + content_path required.
		if len(v.Messages.LineFilter) == 0 {
			return fmt.Errorf("%s: messages.line_filter is required for jsonl driver", prefix)
		}
		if v.Messages.RoleField == "" {
			return fmt.Errorf("%s: messages.role_field is required for jsonl driver", prefix)
		}
		if v.Messages.ContentPath == "" {
			return fmt.Errorf("%s: messages.content_path is required for jsonl driver", prefix)
		}
		// Session ID: either id_from_filename, id (JSON field), or id_field from a specific line type.
		if !v.Sessions.IDFromFilename && v.Sessions.ID == "" {
			return fmt.Errorf("%s: sessions.id or sessions.id_from_filename is required for jsonl driver", prefix)
		}
	} else if v.Messages.Inline {
		if v.Messages.Query != "" {
			return fmt.Errorf("%s: messages.inline and messages.query are mutually exclusive", prefix)
		}
		if v.Sessions.Data == "" {
			return fmt.Errorf("%s: messages.inline requires sessions.data column", prefix)
		}
		if v.Messages.Items == "" {
			return fmt.Errorf("%s: messages.inline requires messages.items (JSON array key)", prefix)
		}
		if len(v.Messages.Variants) == 0 {
			return fmt.Errorf("%s: messages.inline requires at least one variant", prefix)
		}
	} else {
		if v.Messages.Query == "" {
			return fmt.Errorf("%s: messages.query is required (or set messages.inline)", prefix)
		}
		if err := validateSelectOnly(prefix+".messages.query", v.Messages.Query); err != nil {
			return err
		}
		if v.Messages.Role == "" || v.Messages.Content == "" {
			return fmt.Errorf("%s: messages.role and messages.content are required in query mode", prefix)
		}
	}
	// Inline blob decode validation.
	if v.Messages.Decode != "" && v.Messages.Decode != "zstd" {
		return fmt.Errorf("%s: unknown messages.decode %q (want zstd)", prefix, v.Messages.Decode)
	}
	return nil
}

// validateSelectOnly ensures a query starts with SELECT (case-insensitive),
// after stripping leading SQL comments and whitespace. This is a security guard
// to prevent destructive statements in schema files.
func validateSelectOnly(field, query string) error {
	trimmed := strings.TrimSpace(query)
	// Strip leading SQL line comments (-- ...) and block comments (/* ... */).
	for {
		if strings.HasPrefix(trimmed, "--") {
			if nl := strings.IndexByte(trimmed, '\n'); nl >= 0 {
				trimmed = strings.TrimSpace(trimmed[nl+1:])
				continue
			}
			trimmed = ""
			break
		}
		if strings.HasPrefix(trimmed, "/*") {
			if end := strings.Index(trimmed, "*/"); end >= 0 {
				trimmed = strings.TrimSpace(trimmed[end+2:])
				continue
			}
			trimmed = ""
			break
		}
		break
	}
	if trimmed == "" {
		return fmt.Errorf("%s: query is empty", field)
	}
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT") {
		return fmt.Errorf("%s: query must start with SELECT", field)
	}
	return nil
}
