package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ozgurulukir/seek/internal/search"
)

func TestSearchJSON_Schema(t *testing.T) {
	c := &SearchCmd{Query: "test query", JSON: true, Limit: 5}
	results := []search.Result{
		{
			ChunkID: 42, DocumentID: 7, Seq: 2,
			Title: "My Note", Path: "notes/my-note.md",
			Collection: "notes", Content: "some long content",
			Score: 0.9876, ChunkType: search.ChunkTypeText,
			StartLine: 10, EndLine: 25,
		},
	}
	aggs := map[string][]search.Bucket{
		"doc_type:terms": {{Key: "markdown", Count: 2}},
	}

	var buf bytes.Buffer
	// printResultsJSON writes to os.Stdout; capture via encoder refactor test:
	// exercise the mapping logic through a small wrapper instead of stdout.
	out := buildJSONOutput(c, results, aggs)
	if err := json.NewEncoder(&buf).Encode(out); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var parsed struct {
		Query   string `json:"query"`
		Total   int    `json:"total"`
		Results []struct {
			ChunkID     int64   `json:"chunk_id"`
			DocumentID  int64   `json:"document_id"`
			Seq         int     `json:"seq"`
			Title       string  `json:"title"`
			Path        string  `json:"path"`
			Collection  string  `json:"collection"`
			Content     string  `json:"content"`
			ContentKind string  `json:"content_kind"`
			Score       float64 `json:"score"`
			ChunkType   int     `json:"chunk_type"`
			StartLine   int     `json:"start_line"`
			EndLine     int     `json:"end_line"`
			ImagePath   string  `json:"image_path"`
		} `json:"results"`
		Aggs map[string][]struct {
			Key   string `json:"key"`
			Count int    `json:"count"`
		} `json:"aggs"`
	}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Query != "test query" || parsed.Total != 1 {
		t.Errorf("envelope wrong: %+v", parsed)
	}
	r := parsed.Results[0]
	if r.ChunkID != 42 || r.Title != "My Note" || r.StartLine != 10 || r.EndLine != 25 {
		t.Errorf("result fields wrong: %+v", r)
	}
	if r.Score != 0.9876 || r.ChunkType != 0 {
		t.Errorf("score/type wrong: %+v", r)
	}
	if r.ContentKind != "full" {
		t.Errorf("chunk-level result should be content_kind=full, got %q", r.ContentKind)
	}
	if parsed.Aggs["doc_type:terms"][0].Key != "markdown" || parsed.Aggs["doc_type:terms"][0].Count != 2 {
		t.Errorf("aggs wrong: %+v", parsed.Aggs)
	}
}

func TestSearchJSON_EmptyAndNoAggs(t *testing.T) {
	c := &SearchCmd{Query: "zzz", JSON: true}
	out := buildJSONOutput(c, nil, nil)
	data, _ := json.Marshal(out)
	if string(data) != `{"query":"zzz","total":0,"results":[]}` {
		t.Errorf("empty envelope = %s, want omitempty shape", data)
	}
}

func TestEnrichJSONContent_StripsMarkers(t *testing.T) {
	results := []search.Result{
		{ChunkID: -1, Content: ">>>match<<< inside"},
	}
	enrichJSONContent(nil, results)
	if results[0].Content != "match inside" {
		t.Errorf("markers not stripped: %q", results[0].Content)
	}
}

func TestContentKind(t *testing.T) {
	if got := contentKind(search.Result{ChunkID: 42}); got != "full" {
		t.Errorf("chunk-level = %q, want full", got)
	}
	if got := contentKind(search.Result{ChunkID: 0}); got != "snippet" {
		t.Errorf("document-level = %q, want snippet", got)
	}
}
