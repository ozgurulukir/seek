package bench

import "testing"

func TestGameQueryToFTS5(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    string
		wantErr bool
	}{
		{name: "single term", query: "the", want: `("the")`},
		{name: "union is OR", query: "bowel obstruction", want: `("bowel" OR "obstruction")`},
		{name: "intersection is AND", query: "+to +be +or +not", want: `("to" AND "be" AND "or" AND "not")`},
		{name: "phrase", query: `"lord of the rings"`, want: `("lord of the rings")`},
		{name: "two phrase critic", query: `+"the who" +uk`, want: `("the who" AND "uk")`},
		{name: "required phrase only", query: `"search engine"`, want: `("search engine")`},
		{name: "mixed must wins", query: `+engine search`, want: `("engine")`},
		{name: "negation after required", query: "+python -snake", want: `(("python") NOT "snake")`},
		{name: "double negation", query: "+python -snake -monty", want: `(("python") NOT "snake" NOT "monty")`},
		{name: "negation on optional", query: "python -snake", want: `(("python") NOT "snake")`},
		{name: "negation on union", query: "griffith observatory -snake", want: `(("griffith" OR "observatory") NOT "snake")`},
		{name: "negated phrase", query: `+"the who" -uk`, want: `(("the who") NOT "uk")`},
		{name: "negated phrase after union", query: `+"python" -"monty python"`, want: `(("python") NOT "monty python")`},
		{name: "only negation", query: "-snake", wantErr: true},
		{name: "dangling minus", query: "python -", wantErr: true},
		{name: "wildcard", query: "pyth*", wantErr: true},
		{name: "boolean operator", query: "python AND snake", wantErr: true},
		{name: "empty query", query: "   ", wantErr: true},
		{name: "bare plus", query: "+", wantErr: true},
		{name: "unterminated phrase", query: `"open world`, wantErr: true},
		{name: "empty phrase", query: `+""`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GameQueryToFTS5(tt.query)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("GameQueryToFTS5(%q) = %q, want error", tt.query, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("GameQueryToFTS5(%q) error: %v", tt.query, err)
			}
			if got != tt.want {
				t.Fatalf("GameQueryToFTS5(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}
