package bench

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testCorpus is a deterministic mini corpus with hand-computable hit counts.
var testCorpus = []struct{ id, text string }{
	{"doc1", "the quick brown fox jumps over the lazy dog"},
	{"doc2", "the lazy dog sleeps"},
	{"doc3", "a quick query engine example"},
	{"doc4", "search engines index text"},
	{"doc5", "the lord of the rings book"},
}

func corpusJSONL() string {
	var b strings.Builder
	for _, d := range testCorpus {
		b.WriteString(`{"id": "` + d.id + `", "text": "` + d.text + `"}` + "\n")
	}
	return b.String()
}

func ingestTestCorpus(t *testing.T, dbPath string) *IngestStats {
	t.Helper()
	stats, err := Ingest(context.Background(), strings.NewReader(corpusJSONL()), IngestOptions{DBPath: dbPath}, nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return stats
}

func TestIngestIndexesCorpus(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bench.db")
	stats := ingestTestCorpus(t, dbPath)
	if stats.Docs != int64(len(testCorpus)) {
		t.Fatalf("stats.Docs = %d, want %d", stats.Docs, len(testCorpus))
	}
	if stats.Skipped != 0 {
		t.Fatalf("stats.Skipped = %d, want 0", stats.Skipped)
	}
	if stats.Bytes <= 0 {
		t.Fatalf("stats.Bytes = %d, want > 0", stats.Bytes)
	}
}

func TestIngestIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bench.db")
	ingestTestCorpus(t, dbPath)
	ingestTestCorpus(t, dbPath)

	// The union query over "the" matches 3 docs; a duplicated index run must
	// not change counts.
	if got := serveCount(t, dbPath, "the"); got != "3" {
		t.Fatalf(`COUNT "the" = %q, want "3" (index re-run duplicated documents?)`, got)
	}
}

func TestIngestRejectsMalformedLine(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bench.db")
	input := corpusJSONL() + "{not json}\n"
	if _, err := Ingest(context.Background(), strings.NewReader(input), IngestOptions{DBPath: dbPath}, nil); err == nil {
		t.Fatal("Ingest with malformed line: want error, got nil")
	}
}

func TestServeProtocol(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bench.db")
	ingestTestCorpus(t, dbPath)

	tests := []struct {
		name string
		line string
		want string
	}{
		{name: "count term", line: "COUNT\tthe", want: "3"},
		{name: "count union", line: "COUNT\tquick lazy", want: "3"},
		{name: "count intersection", line: "COUNT\t+quick +lazy", want: "1"},
		{name: "count phrase", line: "COUNT\t\"lazy dog\"", want: "2"},
		{name: "count mixed", line: "COUNT\t+\"the lazy\" +dog", want: "2"},
		{name: "top_k answers one", line: "TOP_10\tthe", want: "1"},
		{name: "top_k_count", line: "TOP_100_COUNT\tthe", want: "3"},
		{name: "top_1000_count phrase", line: "TOP_1000_COUNT\t\"lord of the rings\"", want: "1"},
		{name: "unknown command", line: "FACET\tthe", want: "UNSUPPORTED"},
		{name: "no tab", line: "the", want: "UNSUPPORTED"},
		{name: "untranslatable query", line: "COUNT\t+", want: "UNSUPPORTED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Serve(context.Background(), strings.NewReader(tt.line+"\n"), &out, ServeOptions{DBPath: dbPath}); err != nil {
				t.Fatalf("Serve(%q): %v", tt.line, err)
			}
			if got := strings.TrimSpace(out.String()); got != tt.want {
				t.Fatalf("Serve(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestServeMultipleLinesAndFlush(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bench.db")
	ingestTestCorpus(t, dbPath)

	var out bytes.Buffer
	input := "COUNT\tthe\nTOP_10\tsearch\nCOUNT\tengines\n"
	if err := Serve(context.Background(), strings.NewReader(input), &out, ServeOptions{DBPath: dbPath}); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	got := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{"3", "1", "1"}
	if len(got) != len(want) {
		t.Fatalf("Serve wrote %d lines, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func serveCount(t *testing.T, dbPath, query string) string {
	t.Helper()
	var out bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader("COUNT\t"+query+"\n"), &out, ServeOptions{DBPath: dbPath}); err != nil {
		t.Fatalf("Serve COUNT %q: %v", query, err)
	}
	return strings.TrimSpace(out.String())
}

func writeTempResults(t *testing.T, jsonBody string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "results.json")
	if err := os.WriteFile(path, []byte(jsonBody), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
