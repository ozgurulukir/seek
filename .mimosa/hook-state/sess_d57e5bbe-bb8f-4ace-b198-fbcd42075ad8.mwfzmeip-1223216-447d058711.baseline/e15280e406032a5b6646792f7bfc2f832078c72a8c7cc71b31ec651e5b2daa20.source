package search

import (
	"encoding/json"
	"fmt"
	"time"
)

// FieldType represents the type of a schema field.
type FieldType string

const (
	FieldTypeText FieldType = "text"
	FieldTypeDate FieldType = "date"
	FieldTypeInt  FieldType = "int"
	FieldTypeBool FieldType = "bool"
	FieldTypeJSON FieldType = "json"
)

// FieldOptions controls how a field is stored and indexed.
type FieldOptions struct {
	// Indexed means the field is included in the FTS5 index (text fields only).
	Indexed bool `json:"indexed,omitempty"`
	// Stored means the field value is stored in the document for retrieval.
	Stored bool `json:"stored,omitempty"`
	// Fast means the field is stored in the fast field store for sorting/aggregation.
	Fast bool `json:"fast,omitempty"`
}

// FieldDefinition describes a single field in the schema.
type FieldDefinition struct {
	Type    FieldType    `json:"type"`
	Options FieldOptions `json:"options,omitempty"`
}

// Schema is a map of field names to their definitions.
type Schema map[string]FieldDefinition

// SchemaRegistry holds the default schemas for different document types.
type SchemaRegistry struct {
	defaults Schema
}

// NewSchemaRegistry creates a new registry with built-in defaults.
func NewSchemaRegistry() *SchemaRegistry {
	reg := &SchemaRegistry{}
	reg.defaults = Schema{
		"id": {
			Type:    FieldTypeInt,
			Options: FieldOptions{Stored: true, Fast: true},
		},
		"collection_id": {
			Type:    FieldTypeInt,
			Options: FieldOptions{Stored: true, Fast: true},
		},
		"path": {
			Type:    FieldTypeText,
			Options: FieldOptions{Stored: true, Fast: true},
		},
		"title": {
			Type:    FieldTypeText,
			Options: FieldOptions{Indexed: true, Stored: true},
		},
		"content_hash": {
			Type:    FieldTypeText,
			Options: FieldOptions{Stored: true},
		},
		"mtime": {
			Type:    FieldTypeInt,
			Options: FieldOptions{Stored: true, Fast: true},
		},
		"line_count": {
			Type:    FieldTypeInt,
			Options: FieldOptions{Stored: true, Fast: true},
		},
		"created_at": {
			Type:    FieldTypeDate,
			Options: FieldOptions{Stored: true, Fast: true},
		},
		"updated_at": {
			Type:    FieldTypeDate,
			Options: FieldOptions{Stored: true, Fast: true},
		},
		"metadata": {
			Type:    FieldTypeJSON,
			Options: FieldOptions{Stored: true},
		},
	}
	return reg
}

// DefaultSchema returns the default schema.
func (r *SchemaRegistry) DefaultSchema() Schema {
	return r.defaults
}

// ValidateField checks if a field value matches the expected type.
func ValidateField(fd FieldDefinition, value interface{}) error {
	if value == nil {
		return nil
	}

	switch fd.Type {
	case FieldTypeText:
		_, ok := value.(string)
		if !ok {
			return fmt.Errorf("field expects text, got %T", value)
		}
	case FieldTypeDate:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("field expects date string (RFC3339), got %T", value)
		}
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return fmt.Errorf("field expects RFC3339 date, got %q: %w", s, err)
		}
	case FieldTypeInt:
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			// ok
		default:
			return fmt.Errorf("field expects int, got %T", value)
		}
	case FieldTypeBool:
		_, ok := value.(bool)
		if !ok {
			return fmt.Errorf("field expects bool, got %T", value)
		}
	case FieldTypeJSON:
		// JSON can be any JSON-serializable value
		if _, err := json.Marshal(value); err != nil {
			return fmt.Errorf("field expects JSON-serializable value: %w", err)
		}
	}
	return nil
}

// SchemaToJSON returns the schema as a JSON string for CLI output.
func SchemaToJSON(s Schema) (string, error) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
