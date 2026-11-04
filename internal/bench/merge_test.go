package bench

import (
	"strings"
	"testing"
	"time"
)

func fixtureResults() (*BenchmarkResults, *BenchmarkResults) {
	published := &BenchmarkResults{
		Details: map[string][]string{
			"tantivy-0.25": {"Written in Rust."},
		},
		Results: map[string]map[string][]QueryResult{
			"COUNT": {
				"tantivy-0.25": {
					{Query: "the", Tags: []string{"term"}, Count: 3, Duration: []int64{100, 110, 120}},
					{Query: "quick lazy", Tags: []string{"union"}, Count: 3, Duration: []int64{200, 210, 220}},
				},
				"lucene-10.3.0": {
					{Query: "the", Tags: []string{"term"}, Count: 3, Duration: []int64{150, 160, 170}},
					{Query: "quick lazy", Tags: []string{"union"}, Count: 3, Duration: []int64{180, 190, 200}},
				},
			},
			"TOP_100_COUNT": {
				"tantivy-0.25": {
					{Query: "the", Tags: []string{"term"}, Count: 3, Duration: []int64{300}},
					{Query: "quick lazy", Tags: []string{"union"}, Count: 4, Duration: []int64{310}},
				},
			},
		},
	}
	ours := &BenchmarkResults{
		Details: map[string][]string{
			"seek": {"Written in Go. SQLite FTS5."},
		},
		Results: map[string]map[string][]QueryResult{
			"COUNT": {
				"seek": {
					{Query: "the", Tags: []string{"term"}, Count: 3, Duration: []int64{500, 600, 700}},
					{Query: "quick lazy", Tags: []string{"union"}, Count: 3, Duration: []int64{1000, 900, 1100}},
				},
			},
			"TOP_100_COUNT": {
				"seek": {
					{Query: "the", Tags: []string{"term"}, Count: 3, Duration: []int64{800}},
					{Query: "quick lazy", Tags: []string{"union"}, Count: 3, Duration: []int64{900}},
				},
			},
		},
	}
	return ours, published
}

func TestMergeResultsUnionsEnginesAndDetails(t *testing.T) {
	ours, published := fixtureResults()
	merged := MergeResults(ours, published)

	countEngines := orderedEngines(merged, "COUNT")
	if got, want := strings.Join(countEngines, ","), "lucene-10.3.0,tantivy-0.25,seek"; got != want {
		t.Fatalf("COUNT engines = %q, want %q (seek last)", got, want)
	}
	if _, ok := merged.Details["seek"]; !ok {
		t.Fatal("merged.Details missing seek entry")
	}
	if _, ok := merged.Details["tantivy-0.25"]; !ok {
		t.Fatal("merged.Details dropped published entry")
	}
}

func TestCommandTagStatsPercentiles(t *testing.T) {
	ours, published := fixtureResults()
	merged := MergeResults(ours, published)

	stats := CommandTagStats(merged, "COUNT", "")
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
	if got := byEngine["tantivy-0.25"].Average; got != 150 {
		t.Fatalf("tantivy AVERAGE = %v, want 150", got)
	}
	if got := byEngine["seek"].Queries; got != 2 {
		t.Fatalf("seek Queries = %d, want 2", got)
	}

	unionStats := CommandTagStats(merged, "COUNT", "union")
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

func TestCountAgreements(t *testing.T) {
	ours, published := fixtureResults()
	agreements := CountAgreements(ours, published, "tantivy-0.25")

	byCommand := map[string]CountAgreement{}
	for _, a := range agreements {
		byCommand[a.Command] = a
	}
	count := byCommand["COUNT"]
	if count.Matched != 2 || count.Queries != 2 {
		t.Fatalf("COUNT agreement = matched %d/%d, want 2/2", count.Matched, count.Queries)
	}
	topCount := byCommand["TOP_100_COUNT"]
	if topCount.Matched != 1 || topCount.Queries != 2 {
		t.Fatalf("TOP_100_COUNT agreement = matched %d/%d, want 1/2 (published has an intentional mismatch)", topCount.Matched, topCount.Queries)
	}
	if len(topCount.Mismatches) != 1 || topCount.Mismatches[0].Seek != 3 || topCount.Mismatches[0].Reference != 4 {
		t.Fatalf("TOP_100_COUNT mismatch = %+v, want seek 3 vs reference 4", topCount.Mismatches)
	}
}

func TestRenderMarkdownContainsTablesAndDisclosure(t *testing.T) {
	ours, published := fixtureResults()
	merged := MergeResults(ours, published)
	md := RenderMarkdown(merged, ReportOptions{
		GeneratedAt:     time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC),
		SeekVersion:     "v0.5.0",
		SeekMachine:     "test machine",
		PublishedSource: "https://tantivy-search.github.io/bench/results.json",
		ReferenceEngine: "tantivy-0.25",
	})

	for _, want := range []string{
		"# Search benchmark results",
		"### COUNT",
		"### TOP_100_COUNT",
		"| Metric | lucene-10.3.0 | tantivy-0.25 | seek |",
		"**150**", // tantivy COUNT AVERAGE is the fixture's fastest and gets bolded
		"test machine",
		"Hit-count agreement",
		"Reproducing",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("RenderMarkdown missing %q\n---\n%s", want, md)
		}
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
