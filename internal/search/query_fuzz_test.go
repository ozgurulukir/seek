package search

import (
	"strings"
	"testing"
)

func FuzzParseQuery(f *testing.F) {
	for _, seed := range []string{
		"",
		"hello world",
		`"quoted phrase"`,
		"title:hello AND (world OR example)",
		"foo NEAR/3 bar",
		"a\\",
		"(",
		"a OR",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		query, err := ParseQuery(input)
		if err != nil {
			return
		}
		if query == nil {
			if strings.TrimSpace(input) != "" {
				t.Fatalf("ParseQuery(%q) returned nil query without an error", input)
			}
			return
		}

		// Exercise both AST consumers as part of the fuzz target. A parser
		// result with an invalid shape can otherwise survive until search time.
		_ = query.String()
		_, _ = ToFTS5(query)
	})
}
