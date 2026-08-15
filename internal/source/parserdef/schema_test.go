package parserdef

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidateRejectsUnknownDriver ensures only supported drivers pass validation.
func TestValidateRejectsUnknownDriver(t *testing.T) {
	def := &ParserDef{
		Format: 1,
		Name:   "test",
		Sources: []SourceSpec{{
			Driver: "postgres",
			Paths:  []string{"~/db"},
			Versions: []VersionSpec{{
				Version:  1,
				Sessions: SessionsSpec{Query: "SELECT 1", ID: "id"},
				Messages: MessagesSpec{Query: "SELECT 'u' AS role, 'hi' AS content", Role: "role", Content: "content"},
			}},
		}},
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected error for unknown driver, got nil")
	}
}

// TestValidateRejectsNonSelectQuery ensures the SELECT-only guard works.
func TestValidateRejectsNonSelectQuery(t *testing.T) {
	def := &ParserDef{
		Format: 1,
		Name:   "test",
		Sources: []SourceSpec{{
			Driver: "sqlite",
			Paths:  []string{"~/db"},
			Versions: []VersionSpec{{
				Version:  1,
				Sessions: SessionsSpec{Query: "DELETE FROM session", ID: "id"},
				Messages: MessagesSpec{Query: "SELECT 1", Role: "role", Content: "content"},
			}},
		}},
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected error for non-SELECT query, got nil")
	}
}

// TestValidateRejectsUnknownCursorFormat ensures cursor_format is checked.
func TestValidateRejectsUnknownCursorFormat(t *testing.T) {
	def := &ParserDef{
		Format: 1,
		Name:   "test",
		Sources: []SourceSpec{{
			Driver: "sqlite",
			Paths:  []string{"~/db"},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					Query:        "SELECT id, ts FROM s",
					ID:           "id",
					Cursor:       "ts",
					CursorFormat: "iso8601", // unsupported
				},
				Messages: MessagesSpec{Query: "SELECT 1", Role: "role", Content: "content"},
			}},
		}},
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected error for unknown cursor_format, got nil")
	}
}

// TestValidateRejectsInlineWithoutData ensures inline mode requires sessions.data.
func TestValidateRejectsInlineWithoutData(t *testing.T) {
	def := &ParserDef{
		Format: 1,
		Name:   "test",
		Sources: []SourceSpec{{
			Driver: "sqlite",
			Paths:  []string{"~/db"},
			Versions: []VersionSpec{{
				Version:  1,
				Sessions: SessionsSpec{Query: "SELECT id FROM s", ID: "id"},
				Messages: MessagesSpec{Inline: true, Items: "messages"},
			}},
		}},
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected error for inline without sessions.data, got nil")
	}
}

// TestValidateRejectsUnknownMetadataField ensures only known metadata fields are allowed.
func TestValidateRejectsUnknownMetadataField(t *testing.T) {
	def := &ParserDef{
		Format: 1,
		Name:   "test",
		Sources: []SourceSpec{{
			Driver: "sqlite",
			Paths:  []string{"~/db"},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					Query:    "SELECT id FROM s",
					ID:       "id",
					Metadata: map[string]string{"unknown_field": "col"},
				},
				Messages: MessagesSpec{Query: "SELECT 1", Role: "role", Content: "content"},
			}},
		}},
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected error for unknown metadata field, got nil")
	}
}

// TestValidateAcceptsValidSchema ensures a well-formed schema passes.
func TestValidateAcceptsValidSchema(t *testing.T) {
	def := &ParserDef{
		Format: 1,
		Name:   "test",
		Sources: []SourceSpec{{
			Driver: "sqlite",
			Paths:  []string{"~/db"},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					Query:        "SELECT id, title, dir, ts FROM s",
					ID:           "id",
					Title:        "title",
					Cursor:       "ts",
					CursorFormat: "epoch_ms",
					Metadata:     map[string]string{"workspace": "dir"},
				},
				Messages: MessagesSpec{Query: "SELECT 'u' AS role, 'hi' AS content", Role: "role", Content: "content"},
			}},
		}},
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLoadEmbeddedSchema ensures the embedded opencode schema loads and validates.
func TestLoadEmbeddedSchema(t *testing.T) {
	def, err := Load("opencode")
	if err != nil {
		t.Fatalf("Load opencode: %v", err)
	}
	if def.Name != "opencode" {
		t.Errorf("Name = %q, want opencode", def.Name)
	}
	if def.Format != 1 {
		t.Errorf("Format = %d, want 1", def.Format)
	}
	if len(def.Sources) == 0 {
		t.Fatal("no sources")
	}
}

// TestLoadUnknownSchema ensures unknown schemas fail fast.
func TestLoadUnknownSchema(t *testing.T) {
	_, err := Load("nonexistent-parser-xyz")
	if err == nil {
		t.Fatal("expected error for unknown schema, got nil")
	}
}

// TestLoadUserOverride ensures a user override takes precedence over the embedded schema.
func TestLoadUserOverride(t *testing.T) {
	// Set up a temp HOME so parserOverrideDir points there.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Write a user override for "opencode" with a different description.
	overrideDir := filepath.Join(tmpHome, ".config", "seek", "parsers")
	if err := os.MkdirAll(overrideDir, 0755); err != nil {
		t.Fatal(err)
	}
	overrideContent := []byte(`
format: 1
name: opencode
description: OVERRIDE VERSION
sources:
  - driver: sqlite
    paths: ["~/test.db"]
    versions:
      - version: 1
        sessions:
          query: SELECT id FROM s
          id: id
        messages:
          query: SELECT 'u' AS role, 'hi' AS content
          role: role
          content: content
`)
	if err := os.WriteFile(filepath.Join(overrideDir, "opencode.yaml"), overrideContent, 0644); err != nil {
		t.Fatal(err)
	}

	def, err := Load("opencode")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if def.Description != "OVERRIDE VERSION" {
		t.Errorf("Description = %q, want OVERRIDE VERSION", def.Description)
	}
}

// TestListReturnsSchemas ensures List returns at least the embedded opencode schema.
func TestListReturnsSchemas(t *testing.T) {
	defs, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, d := range defs {
		if d.Name == "opencode" {
			found = true
			if !d.Embedded {
				t.Error("opencode should be marked embedded")
			}
		}
	}
	if !found {
		t.Fatal("opencode schema not found in List()")
	}
}

// TestValidateSelectOnly handles SQL comments and whitespace.
func TestValidateSelectOnly(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"plain select", "SELECT 1", false},
		{"leading whitespace", "   SELECT 1", false},
		{"newline", "\nSELECT 1", false},
		{"line comment", "-- comment\nSELECT 1", false},
		{"block comment", "/* hi */ SELECT 1", false},
		{"delete", "DELETE FROM t", true},
		{"insert", "INSERT INTO t VALUES(1)", true},
		{"drop", "DROP TABLE t", true},
		{"empty", "", true},
		{"only comment", "-- nothing", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSelectOnly("field", tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSelectOnly(%q) err=%v, wantErr=%v", tt.query, err, tt.wantErr)
			}
		})
	}
}
