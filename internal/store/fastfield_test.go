package store

import (
	"testing"
)

func TestFastFieldSetGet(t *testing.T) {
	db := newTestStore(t)
	defer db.Close()

	ff := NewFastFieldStore(db.DB())

	// Set a string value
	if err := ff.Set(1, "title", "hello"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get it back
	val, err := ff.Get(1, "title")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if val != "hello" {
		t.Errorf("expected 'hello', got %v", val)
	}
}

func TestFastFieldSetGetInt(t *testing.T) {
	db := newTestStore(t)
	defer db.Close()

	ff := NewFastFieldStore(db.DB())

	if err := ff.Set(1, "line_count", 42); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, err := ff.Get(1, "line_count")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if val != float64(42) {
		t.Errorf("expected 42, got %v", val)
	}
}

func TestFastFieldSetGetDate(t *testing.T) {
	db := newTestStore(t)
	defer db.Close()

	ff := NewFastFieldStore(db.DB())

	date := "2024-01-01T00:00:00Z"
	if err := ff.Set(1, "created_at", date); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, err := ff.Get(1, "created_at")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if val != date {
		t.Errorf("expected %q, got %v", date, val)
	}
}

func TestFastFieldBatchGet(t *testing.T) {
	db := newTestStore(t)
	defer db.Close()

	ff := NewFastFieldStore(db.DB())

	// Set values for multiple docs
	if err := ff.Set(1, "score", 0.9); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := ff.Set(2, "score", 0.8); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := ff.Set(3, "score", 0.7); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Batch get
	vals, err := ff.BatchGet([]int64{1, 2, 3}, "score")
	if err != nil {
		t.Fatalf("BatchGet failed: %v", err)
	}

	if len(vals) != 3 {
		t.Fatalf("expected 3 values, got %d", len(vals))
	}

	if vals[1] != float64(0.9) {
		t.Errorf("expected doc 1 score 0.9, got %v", vals[1])
	}
	if vals[2] != float64(0.8) {
		t.Errorf("expected doc 2 score 0.8, got %v", vals[2])
	}
	if vals[3] != float64(0.7) {
		t.Errorf("expected doc 3 score 0.7, got %v", vals[3])
	}
}

func TestFastFieldBatchGetMissing(t *testing.T) {
	db := newTestStore(t)
	defer db.Close()

	ff := NewFastFieldStore(db.DB())

	// Batch get for non-existent docs
	vals, err := ff.BatchGet([]int64{1, 2, 3}, "nonexistent")
	if err != nil {
		t.Fatalf("BatchGet failed: %v", err)
	}

	if len(vals) != 0 {
		t.Errorf("expected 0 values for missing field, got %d", len(vals))
	}
}

func TestFastFieldDelete(t *testing.T) {
	db := newTestStore(t)
	defer db.Close()

	ff := NewFastFieldStore(db.DB())

	if err := ff.Set(1, "title", "hello"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if err := ff.Delete(1, "title"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	val, err := ff.Get(1, "title")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if val != nil {
		t.Errorf("expected nil after delete, got %v", val)
	}
}

func TestFastFieldDeleteForDocument(t *testing.T) {
	db := newTestStore(t)
	defer db.Close()

	ff := NewFastFieldStore(db.DB())

	if err := ff.Set(1, "title", "hello"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := ff.Set(1, "score", 0.9); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if err := ff.DeleteForDocument(1); err != nil {
		t.Fatalf("DeleteForDocument failed: %v", err)
	}

	val1, _ := ff.Get(1, "title")
	val2, _ := ff.Get(1, "score")

	if val1 != nil || val2 != nil {
		t.Error("expected all fields deleted for document")
	}
}

func TestFastFieldOverwrite(t *testing.T) {
	db := newTestStore(t)
	defer db.Close()

	ff := NewFastFieldStore(db.DB())

	if err := ff.Set(1, "title", "hello"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Overwrite
	if err := ff.Set(1, "title", "world"); err != nil {
		t.Fatalf("Set overwrite failed: %v", err)
	}

	val, err := ff.Get(1, "title")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if val != "world" {
		t.Errorf("expected 'world', got %v", val)
	}
}

func TestFastFieldJSONValue(t *testing.T) {
	db := newTestStore(t)
	defer db.Close()

	ff := NewFastFieldStore(db.DB())

	obj := map[string]interface{}{"key": "value", "count": 42}
	if err := ff.Set(1, "metadata", obj); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, err := ff.Get(1, "metadata")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	m, ok := val.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}

	if m["key"] != "value" {
		t.Errorf("expected key='value', got %v", m["key"])
	}
}

func BenchmarkFastFieldBatchGet(b *testing.B) {
	s, err := Open(b.TempDir() + "/test.db")
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ff := NewFastFieldStore(s.DB())

	docIDs := make([]int64, 100)
	for i := 0; i < 100; i++ {
		docIDs[i] = int64(i)
		if err := ff.Set(int64(i), "score", float64(i)); err != nil {
			b.Fatalf("Set failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ff.BatchGet(docIDs, "score")
		if err != nil {
			b.Fatalf("BatchGet failed: %v", err)
		}
	}
}
