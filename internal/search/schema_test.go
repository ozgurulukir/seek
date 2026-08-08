package search

import (
	"testing"
	"time"
)

func TestNewSchemaRegistry(t *testing.T) {
	reg := NewSchemaRegistry()
	schema := reg.DefaultSchema()

	if schema == nil {
		t.Fatal("expected non-nil schema")
	}

	if len(schema) == 0 {
		t.Fatal("expected non-empty schema")
	}
}

func TestDefaultSchemaFields(t *testing.T) {
	reg := NewSchemaRegistry()
	schema := reg.DefaultSchema()

	requiredFields := []string{"id", "collection_id", "path", "title", "content_hash", "mtime", "line_count", "created_at", "updated_at", "metadata"}
	for _, field := range requiredFields {
		if _, ok := schema[field]; !ok {
			t.Errorf("expected field %q in default schema", field)
		}
	}
}

func TestValidateFieldText(t *testing.T) {
	fd := FieldDefinition{Type: FieldTypeText}
	if err := ValidateField(fd, "hello"); err != nil {
		t.Errorf("expected no error for text field, got: %v", err)
	}
	if err := ValidateField(fd, 123); err == nil {
		t.Error("expected error for int value in text field")
	}
}

func TestValidateFieldDate(t *testing.T) {
	fd := FieldDefinition{Type: FieldTypeDate}
	if err := ValidateField(fd, "2024-01-01T00:00:00Z"); err != nil {
		t.Errorf("expected no error for valid date, got: %v", err)
	}
	if err := ValidateField(fd, "not-a-date"); err == nil {
		t.Error("expected error for invalid date string")
	}
	if err := ValidateField(fd, 123); err == nil {
		t.Error("expected error for int value in date field")
	}
}

func TestValidateFieldInt(t *testing.T) {
	fd := FieldDefinition{Type: FieldTypeInt}
	if err := ValidateField(fd, 42); err != nil {
		t.Errorf("expected no error for int field, got: %v", err)
	}
	if err := ValidateField(fd, int64(42)); err != nil {
		t.Errorf("expected no error for int64 field, got: %v", err)
	}
	if err := ValidateField(fd, "hello"); err == nil {
		t.Error("expected error for string value in int field")
	}
}

func TestValidateFieldBool(t *testing.T) {
	fd := FieldDefinition{Type: FieldTypeBool}
	if err := ValidateField(fd, true); err != nil {
		t.Errorf("expected no error for bool field, got: %v", err)
	}
	if err := ValidateField(fd, "true"); err == nil {
		t.Error("expected error for string value in bool field")
	}
}

func TestValidateFieldJSON(t *testing.T) {
	fd := FieldDefinition{Type: FieldTypeJSON}
	if err := ValidateField(fd, map[string]interface{}{"key": "value"}); err != nil {
		t.Errorf("expected no error for JSON field, got: %v", err)
	}
	if err := ValidateField(fd, []int{1, 2, 3}); err != nil {
		t.Errorf("expected no error for JSON array field, got: %v", err)
	}
}

func TestValidateFieldNil(t *testing.T) {
	fd := FieldDefinition{Type: FieldTypeText}
	if err := ValidateField(fd, nil); err != nil {
		t.Errorf("expected no error for nil value, got: %v", err)
	}
}

func TestSchemaToJSON(t *testing.T) {
	reg := NewSchemaRegistry()
	schema := reg.DefaultSchema()

	json, err := SchemaToJSON(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(json) == 0 {
		t.Error("expected non-empty JSON output")
	}
}

func TestCompareFastFieldValues(t *testing.T) {
	tests := []struct {
		a, b     interface{}
		expected int
	}{
		{"a", "b", -1},
		{"b", "a", 1},
		{"a", "a", 0},
		{float64(1), float64(2), -1},
		{float64(2), float64(1), 1},
		{float64(1), float64(1), 0},
		{1, 2, -1}, // ints get converted to float64 via interface{}
		{2, 1, 1},
	}

	for _, tt := range tests {
		got := compareFastFieldValues(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("compareFastFieldValues(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.expected)
		}
	}
}

func TestDefaultSchemaDateField(t *testing.T) {
	reg := NewSchemaRegistry()
	schema := reg.DefaultSchema()

	createdAt, ok := schema["created_at"]
	if !ok {
		t.Fatal("expected created_at field in schema")
	}

	if createdAt.Type != FieldTypeDate {
		t.Errorf("expected created_at type to be date, got %s", createdAt.Type)
	}

	if !createdAt.Options.Fast {
		t.Error("expected created_at to be a fast field")
	}
}

func TestDefaultSchemaIntField(t *testing.T) {
	reg := NewSchemaRegistry()
	schema := reg.DefaultSchema()

	lineCount, ok := schema["line_count"]
	if !ok {
		t.Fatal("expected line_count field in schema")
	}

	if lineCount.Type != FieldTypeInt {
		t.Errorf("expected line_count type to be int, got %s", lineCount.Type)
	}

	if !lineCount.Options.Fast {
		t.Error("expected line_count to be a fast field")
	}
}

func TestValidateFieldWithTime(t *testing.T) {
	fd := FieldDefinition{Type: FieldTypeDate}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := ValidateField(fd, now); err != nil {
		t.Errorf("expected no error for RFC3339 date, got: %v", err)
	}
}
