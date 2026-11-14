package bench

import (
	"strings"
	"testing"
	"time"
)

// fixtureResults models one local harness run: every engine in the file was
// measured on the same machine in the same run.
func fixtureResults() *BenchmarkResults {
	return &BenchmarkResults{
		Details: map[string][]string{
			"tantivy-0.22": {"Written in Rust."},
			"seek":         {"Written in Go. SQLite FTS5."},
		},
		Results: map[string]map[string][]QueryResult{
			"COUNT": {
				"tantivy-0.22": {
					{Query: "the", Tags: []string{"term"}, Count: 3, Duration: []int64{100, 110, 120}},
					{Query: "quick lazy", Tags: []string{"union"}, Count: 3, Duration: []int64{200, 210, 220}},
				},
				"seek": {
					{Query: "the", Tags: []string{"term"}, Count: 3, Duration: []int64{500, 600, 700}},
					{Query: "quick lazy", Tags: []string{"union"}, Count: 3, Duration: []int64{1000, 900, 1100}},
				},
			},
			"TOP_100_COUNT": {
				"tantivy-0.22": {
					{Query: "the", Tags: []string{"term"}, Count: 3, Duration: []int64{300}},
					{Query: "quick lazy", Tags: []string{"union"}, Count: 4, Duration: []int64{310}},
				},
				"seek": {
					{Query: "the", Tags: []string{"term"}, Count: 3, Duration: []int64{800}},
					{Query: "quick lazy", Tags: []string{"union"}, Count: 3, Duration: []int64{900}},
				},
			},
		},
	}
}

func TestCommandTagStatsPercentiles(t *testing.T) {
	results := fixtureResults()

	stats := CommandTagStats(results, "COUNT", "")
	if got, want := strings.Join([]string{stats[0].Engine, stats[1].Engine}, ","), "tantivy-0.22,seek"; got != want {
		t.Fatalf("COUNT engines = %q, want %q (seek last)", got, want)
	}
	byEngine := map[string]EngineStats{}
	for _, s := range stats {
		byEngine[s.Engine] = s
	}
	// Per-query minima: seek {500, 900} → avg 700, p50 500 (nearest-rank
	// picks the lower middle for an even count); tantivy {100, 200} → avg
	// 150, p50 100.
	if got := byEngine["seek"].Average; got != 700 {
		t.Fatalf("seek AVERAGE = %v, want 700", got)
	}
	if got := byEngine["seek"].P50; got != 500 {
		t.Fatalf("seek P50 = %v, want 500", got)
	}
	if got := byEngine["tantivy-0.22"].Average; got != 150 {
		t.Fatalf("tantivy AVERAGE = %v, want 150", got)
	}
	if got := byEngine["seek"].Queries; got != 2 {
		t.Fatalf("seek Queries = %d, want 2", got)
	}

	unionStats := CommandTagStats(results, "COUNT", "union")
	for _, s := range unionStats {
		if s.Queries != 1 {
			t.Fatalf("%s union Queries = %d, want 1", s.Engine, s.Queries)
		}
	}
}

func TestPercentileNearestRank(t *testing.T) {
	sorted := []float64{1, 2, 3, 4, 5}
	cases := []struct{ p, want float64 }{{0.5, 3}, {0.9, 5}, {0.99, 5}, {0.0, 1}}
	for _, c := range cases {
		if got := percentile(sorted, c.p); got != c.want {
			t.Fatalf("percentile(%v, %v) = %v, want %v", sorted, c.p, got, c.want)
		}
	}
}

func TestCountAgreementsSameRun(t *testing.T) {
	results := fixtureResults()
	agreements := CountAgreements(results, "tantivy-0.22")

	byCommand := map[string]CountAgreement{}
	for _, a := range agreements {
		byCommand[a.Command] = a
	}
	count := byCommand["COUNT"]
	if count.Matched != 2 || count.Compared != 2 {
		t.Fatalf("COUNT agreement = matched %d/%d, want 2/2", count.Matched, count.Compared)
	}
	topCount := byCommand["TOP_100_COUNT"]
	if topCount.Matched != 1 || topCount.Compared != 2 {
		t.Fatalf("TOP_100_COUNT agreement = matched %d/%d, want 1/2 (fixture has an intentional mismatch)", topCount.Matched, topCount.Compared)
	}
	if len(topCount.Mismatches) != 1 || topCount.Mismatches[0].Seek != 3 || topCount.Mismatches[0].Reference != 4 {
		t.Fatalf("TOP_100_COUNT mismatch = %+v, want seek 3 vs reference 4", topCount.Mismatches)
	}
}

func TestRenderMarkdownContainsTablesAndDisclosure(t *testing.T) {
	results := fixtureResults()
	md := RenderMarkdown(results, ReportOptions{
		GeneratedAt:     time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC),
		SeekVersion:     "v0.6.7",
		SeekMachine:     "test machine",
		ReferenceEngine: "tantivy-0.22",
	})

	for _, want := range []string{
		"# Search benchmark results",
		"### COUNT",
		"### TOP_100_COUNT",
		"| Metric | tantivy-0.22 | seek |",
		"**150**", // tantivy COUNT AVERAGE is the fixture's fastest and gets bolded
		"measured on one machine in one harness run",
		"test machine",
		"Hit-count agreement",
		"tantivy-0.22",
		"Reproducing",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("RenderMarkdown missing %q\n---\n%s", want, md)
		}
	}
	// Published-only columns must never appear: the report renders exactly
	// the engines present in the local results file.
	for _, banned := range []string{"tantivy-0.25", "lucene-10.3.0", "AWS"} {
		if strings.Contains(md, banned) {
			t.Fatalf("RenderMarkdown contains cross-hardware content %q:\n%s", banned, md)
		}
	}
}

func TestRenderMarkdownWithoutReferenceEngine(t *testing.T) {
	results := fixtureResults()
	// Drop the reference engine: the agreement table must degrade to the
	// empty placeholder, not crash.
	delete(results.Results["COUNT"], "tantivy-0.22")
	delete(results.Results["TOP_100_COUNT"], "tantivy-0.22")
	md := RenderMarkdown(results, ReportOptions{ReferenceEngine: "tantivy-0.22"})
	if !strings.Contains(md, "Hit-count agreement") || !strings.Contains(md, "| — |") {
		t.Fatalf("expected empty agreement placeholder without reference engine, got:\n%s", md)
	}
}

func TestLoadBenchmarkResults(t *testing.T) {
	path := writeTempResults(t, `{"details":{},"results":{"COUNT":{"seek":[{"query":"the","tags":["term"],"count":1,"duration":[10,20]}]}}}`)
	r, err := LoadBenchmarkResults(path)
	if err != nil {
		t.Fatalf("LoadBenchmarkResults: %v", err)
	}
	if got := r.Results["COUNT"]["seek"][0].Count; got != 1 {
		t.Fatalf("Count = %d, want 1", got)
	}
}

func TestLoadBenchmarkResultsRejectsEmpty(t *testing.T) {
	path := writeTempResults(t, `{"details":{},"results":{}}`)
	if _, err := LoadBenchmarkResults(path); err == nil {
		t.Fatal("LoadBenchmarkResults on empty results: want error, got nil")
	}
}
