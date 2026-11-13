package search

import (
	"testing"
)

func TestAutocompleteBasic(t *testing.T) {
	terms := []string{"hello", "help", "helm", "world", "word"}
	ac, err := NewAutocomplete(terms)
	if err != nil {
		t.Fatalf("NewAutocomplete: %v", err)
	}
	defer ac.Close()

	results := ac.Complete("hel", 0)
	if len(results) != 3 {
		t.Fatalf("Complete(hel) len = %d, want 3", len(results))
	}
	// Sort results for comparison (FST iterator order may vary)
	expected := []string{"hello", "help", "helm"}
	for i := range results {
		found := false
		for j := range expected {
			if results[i] == expected[j] {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Complete(hel)[%d] = %q, not in expected set %v", i, results[i], expected)
		}
	}
}

func TestAutocompleteMaxResults(t *testing.T) {
	terms := []string{"hello", "help", "helm", "world", "word"}
	ac, err := NewAutocomplete(terms)
	if err != nil {
		t.Fatalf("NewAutocomplete: %v", err)
	}
	defer ac.Close()

	results := ac.Complete("", 2)
	if len(results) != 2 {
		t.Fatalf("Complete('') with max=2 len = %d, want 2", len(results))
	}
}

func TestAutocompleteNoMatch(t *testing.T) {
	terms := []string{"hello", "help", "helm"}
	ac, err := NewAutocomplete(terms)
	if err != nil {
		t.Fatalf("NewAutocomplete: %v", err)
	}
	defer ac.Close()

	results := ac.Complete("xyz", 0)
	if len(results) != 0 {
		t.Errorf("Complete(xyz) len = %d, want 0", len(results))
	}
}

func TestAutocompleteEmptyTerms(t *testing.T) {
	ac, err := NewAutocomplete(nil)
	if err != nil {
		t.Fatalf("NewAutocomplete(nil): %v", err)
	}
	defer ac.Close()

	results := ac.Complete("hel", 0)
	if len(results) != 0 {
		t.Errorf("Complete(hel) with empty terms len = %d, want 0", len(results))
	}
}

func TestAutocompleteDeduplicate(t *testing.T) {
	terms := []string{"hello", "hello", "help", "help", "helm"}
	ac, err := NewAutocomplete(terms)
	if err != nil {
		t.Fatalf("NewAutocomplete: %v", err)
	}
	defer ac.Close()

	results := ac.Complete("hel", 0)
	if len(results) != 3 {
		t.Fatalf("Complete(hel) len = %d, want 3 (deduplicated)", len(results))
	}
}

func TestAutocompleteLen(t *testing.T) {
	terms := []string{"hello", "help", "helm"}
	ac, err := NewAutocomplete(terms)
	if err != nil {
		t.Fatalf("NewAutocomplete: %v", err)
	}
	defer ac.Close()

	if ac.Len() != 3 {
		t.Errorf("Len() = %d, want 3", ac.Len())
	}
}

func TestAutocompleteNil(t *testing.T) {
	var ac *Autocomplete
	results := ac.Complete("hel", 0)
	if len(results) != 0 {
		t.Errorf("nil.Complete(hel) len = %d, want 0", len(results))
	}
}
