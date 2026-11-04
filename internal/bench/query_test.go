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
