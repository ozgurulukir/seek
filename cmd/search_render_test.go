package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/search"
)

// TestWriteAggregations_StableOrder pins deterministic human rendering: the
// aggregation map has no stable iteration order, so repeated renders of the
// same map must still produce identical output with specs sorted.
func TestWriteAggregations_StableOrder(t *testing.T) {
	aggs := map[string][]search.Bucket{
		"created_at:histogram:month": {{Key: "2026-01", Count: 3}},
		"doc_type:terms":             {{Key: "markdown", Count: 5}, {Key: "code", Count: 2}},
		"lang:terms:20":              {{Key: "en", Count: 7}},
		"tags:terms":                 {{Key: "go", Count: 4}},
	}

	var first string
	for i := 0; i < 100; i++ {
		var buf bytes.Buffer
		writeAggregations(&buf, aggs)
		got := buf.String()
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("iteration %d produced different output:\n%s\n---\n%s", i, first, got)
		}
	}

	wantOrder := []string{
		"created_at:histogram:month",
		"doc_type:terms",
		"lang:terms:20",
		"tags:terms",
	}
	pos := 0
	for _, spec := range wantOrder {
		idx := strings.Index(first, "  "+spec+":\n")
		if idx < pos {
			t.Fatalf("spec %q out of order in:\n%s", spec, first)
		}
		pos = idx + 1
	}
}
