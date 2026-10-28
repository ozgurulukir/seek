package store

import "strings"

const maxPrecomputedPlaceholders = 2000

var precomputedPlaceholders = func() string {
	b := make([]byte, maxPrecomputedPlaceholders*2-1)
	for i := 0; i < len(b); i += 2 {
		b[i] = '?'
		if i+1 < len(b) {
			b[i+1] = ','
		}
	}
	return string(b)
}()

// sqlPlaceholders returns a comma-separated string of n SQL parameter placeholders "?,?,...".
// For n <= 2000, it returns a substring slice of a precomputed string with 0 allocations.
func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	if n <= maxPrecomputedPlaceholders {
		return precomputedPlaceholders[:n*2-1]
	}
	placeholders := make([]string, n)
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return strings.Join(placeholders, ",")
}
