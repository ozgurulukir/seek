package search

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewSearchResults(t *testing.T) {
	results := NewSearchResults([]Result{
		{ChunkID: 42, DocumentID: 7, Seq: 2, Title: "My Note", Score: 0.5, ChunkType: ChunkTypeText, StartLine: 10, EndLine: 25},
		{ChunkID: 0, DocumentID: 8, Title: "Doc Hit", Score: 0.25},
	})
	if results[0].ContentKind != "full" {
		t.Errorf("chunk-level content_kind = %q, want full", results[0].ContentKind)
	}
	if results[1].ContentKind != "snippet" {
		t.Errorf("document-level content_kind = %q, want snippet", results[1].ContentKind)
	}
	b, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"image_path"`) {
		t.Errorf("empty image_path must be omitted: %s", b)
	}
}

func TestNewSearchResultsEmptyIsNotEmptyNull(t *testing.T) {
	b, err := json.Marshal(NewSearchResults(nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[]" {
		t.Errorf("empty results marshal as %s, want []", b)
	}
}

func TestNewSearchOutput(t *testing.T) {
	out := NewSearchOutput("zzz", nil, nil)
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"query":"zzz","total":0,"results":[]}` {
		t.Errorf("empty envelope = %s, want pinned omitempty shape", data)
	}

	out = NewSearchOutput("q", []Result{{ChunkID: 1}}, map[string][]Bucket{
		"type:terms": {{Key: "markdown", Count: 2}},
	})
	if out.Aggs["type:terms"][0].Key != "markdown" || out.Aggs["type:terms"][0].Count != 2 {
		t.Errorf("aggs mapping wrong: %+v", out.Aggs)
	}
}

func TestAutocompleteOutput(t *testing.T) {
	b, err := json.Marshal(NewAutocompleteOutput("x", []string{"xa", "xb"}))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Query       string   `json:"query"`
		Suggestions []string `json:"suggestions"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("parse: %v (%s)", err, b)
	}
	if parsed.Query != "x" || len(parsed.Suggestions) != 2 {
		t.Errorf("autocomplete shape wrong: %+v", parsed)
	}
}
