package search

import (
	"encoding/json"
	"sort"
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

// TestCompareFastFieldValuesMixedTypes pins the M12 contract: a mixed-type
// column must sort deterministically. The old comparator returned "a > b"
// for both (a,b) and (b,a) across types — antisymmetry broken, sort.Slice
// order arbitrary (review 2026-09-17 M12).
func TestCompareFastFieldValuesMixedTypes(t *testing.T) {
	values := []interface{}{"en", float64(42), "zh", float64(7), json.Number("3"), "a", float64(100)}

	// Antisymmetry: compare(a,b) must be the negation of compare(b,a).
	for _, a := range values {
		for _, b := range values {
			ab := compareFastFieldValues(a, b)
			ba := compareFastFieldValues(b, a)
			if ab == -ba || (ab == 0 && ba == 0) {
				continue
			}
			t.Errorf("antisymmetry violated: compare(%v,%v)=%d but compare(%v,%v)=%d", a, b, ab, b, a, ba)
		}
	}

	sorted := append([]interface{}(nil), values...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return compareFastFieldValues(sorted[i], sorted[j]) < 0
	})
	// Numbers first (ascending), then strings (ascending).
	want := []interface{}{json.Number("3"), float64(7), float64(42), float64(100), "a", "en", "zh"}
	for i := range want {
		if compareFastFieldValues(sorted[i], want[i]) != 0 {
			t.Fatalf("sorted[%d] = %v, want %v (full: %v)", i, sorted[i], want[i], sorted)
		}
	}
}

func TestDefaultSchema_ZeroValueRegistry(t *testing.T) {
	var reg SchemaRegistry
	schema := reg.DefaultSchema()
	if schema != nil {
		t.Errorf("expected nil schema from zero-value SchemaRegistry, got %v", schema)
	}
}

func TestDefaultSchema_AllFieldDefinitions(t *testing.T) {
	reg := NewSchemaRegistry()
	schema := reg.DefaultSchema()

	expectedFields := map[string]FieldDefinition{
		"id":            {Type: FieldTypeInt, Options: FieldOptions{Stored: true, Fast: true}},
		"collection_id": {Type: FieldTypeInt, Options: FieldOptions{Stored: true, Fast: true}},
		"path":          {Type: FieldTypeText, Options: FieldOptions{Stored: true, Fast: true}},
		"title":         {Type: FieldTypeText, Options: FieldOptions{Indexed: true, Stored: true}},
		"content_hash":  {Type: FieldTypeText, Options: FieldOptions{Stored: true}},
		"mtime":         {Type: FieldTypeInt, Options: FieldOptions{Stored: true, Fast: true}},
		"line_count":    {Type: FieldTypeInt, Options: FieldOptions{Stored: true, Fast: true}},
		"created_at":    {Type: FieldTypeDate, Options: FieldOptions{Stored: true, Fast: true}},
		"updated_at":    {Type: FieldTypeDate, Options: FieldOptions{Stored: true, Fast: true}},
		"metadata":      {Type: FieldTypeJSON, Options: FieldOptions{Stored: true}},
	}

	if len(schema) != len(expectedFields) {
		t.Errorf("expected %d fields in default schema, got %d", len(expectedFields), len(schema))
	}

	for name, expectedDef := range expectedFields {
		def, ok := schema[name]
		if !ok {
			t.Errorf("missing field %q in default schema", name)
			continue
		}

		if def.Type != expectedDef.Type {
			t.Errorf("field %q type = %s, want %s", name, def.Type, expectedDef.Type)
		}
		if def.Options.Indexed != expectedDef.Options.Indexed {
			t.Errorf("field %q Indexed = %v, want %v", name, def.Options.Indexed, expectedDef.Options.Indexed)
		}
		if def.Options.Stored != expectedDef.Options.Stored {
			t.Errorf("field %q Stored = %v, want %v", name, def.Options.Stored, expectedDef.Options.Stored)
		}
		if def.Options.Fast != expectedDef.Options.Fast {
			t.Errorf("field %q Fast = %v, want %v", name, def.Options.Fast, expectedDef.Options.Fast)
		}
	}
}

func TestDefaultSchema_Consistency(t *testing.T) {
	reg := NewSchemaRegistry()
	s1 := reg.DefaultSchema()
	s2 := reg.DefaultSchema()

	if len(s1) != len(s2) {
		t.Fatalf("mismatch in schema lengths across calls: %d vs %d", len(s1), len(s2))
	}

	for k, v1 := range s1 {
		v2, ok := s2[k]
		if !ok {
			t.Errorf("key %q missing from second call to DefaultSchema", k)
			continue
		}
		if v1 != v2 {
			t.Errorf("field %q definition changed between calls: %v vs %v", k, v1, v2)
		}
	}
}
