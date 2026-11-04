package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmitQueriesDeterministic(t *testing.T) {
	path := writeTempResults(t, `{
		"details": {},
		"results": {
			"COUNT": {
				"seek": [
					{"query": "the", "tags": ["term"], "count": 1, "duration": [1]},
					{"query": "quick lazy", "tags": ["union"], "count": 2, "duration": [2]}
				]
			},
			"TOP_10": {
				"seek": [
					{"query": "the", "tags": ["term"], "count": 1, "duration": [1]}
				]
			}
		}
	}`)
	outPath := filepath.Join(t.TempDir(), "sub", "queries.txt")
	if err := EmitQueries(path, outPath); err != nil {
		t.Fatalf("EmitQueries: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read queries: %v", err)
	}
	// COUNT sorts before TOP_10, so its query list is the source; order must
	// match the stored order and tags must round-trip.
	got := strings.TrimSpace(string(data))
	want := `{"query":"the","tags":["term"]}` + "\n" + `{"query":"quick lazy","tags":["union"]}`
	if got != want {
		t.Fatalf("queries.txt =\n%s\nwant\n%s", got, want)
	}
}
