package store

import (
	"strings"
	"testing"
)

func TestSQLPlaceholders(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{-1, ""},
		{0, ""},
		{1, "?"},
		{2, "?,?"},
		{3, "?,?,?"},
		{5, "?,?,?,?,?"},
	}

	for _, tt := range tests {
		got := sqlPlaceholders(tt.n)
		if got != tt.want {
			t.Errorf("sqlPlaceholders(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}

	// Test 500 placeholders
	got500 := sqlPlaceholders(500)
	parts500 := strings.Split(got500, ",")
	if len(parts500) != 500 {
		t.Errorf("len(sqlPlaceholders(500) split) = %d, want 500", len(parts500))
	}
	for _, p := range parts500 {
		if p != "?" {
			t.Errorf("sqlPlaceholders(500) element = %q, want ?", p)
		}
	}

	// Test max precomputed edge case (2000)
	got2000 := sqlPlaceholders(2000)
	parts2000 := strings.Split(got2000, ",")
	if len(parts2000) != 2000 {
		t.Errorf("len(sqlPlaceholders(2000) split) = %d, want 2000", len(parts2000))
	}

	// Test fallback case (>2000)
	got2001 := sqlPlaceholders(2001)
	parts2001 := strings.Split(got2001, ",")
	if len(parts2001) != 2001 {
		t.Errorf("len(sqlPlaceholders(2001) split) = %d, want 2001", len(parts2001))
	}
}

func BenchmarkSQLPlaceholders(b *testing.B) {
	b.Run("strings.Join", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			placeholders := make([]string, 500)
			for j := range placeholders {
				placeholders[j] = "?"
			}
			_ = strings.Join(placeholders, ",")
		}
	})

	b.Run("sqlPlaceholders", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = sqlPlaceholders(500)
		}
	})
}
