package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// SeekEngineName is the engine name seek reports under in results.json —
// it must equal the directory name under third_party/search-benchmark-game/engines/.
const SeekEngineName = "seek"

// KnownCommands lists the commands published on tantivy-search.github.io/bench.
var KnownCommands = []string{"TOP_10", "TOP_100", "TOP_1000", "TOP_100_COUNT", "COUNT"}

// BenchmarkResults mirrors the search-benchmark-game results.json schema:
// raw per-query duration arrays (µs, ascending, one entry per measured
// pass). All statistics are derived from these arrays, never stored — the
// same design choice the published page makes.
type BenchmarkResults struct {
	Details map[string][]string                 `json:"details"`
	Results map[string]map[string][]QueryResult `json:"results"`
}

// QueryResult is one query's outcome under one engine and command.
type QueryResult struct {
	Query    string   `json:"query"`
	Tags     []string `json:"tags"`
	Count    int      `json:"count"`
	Duration []int64  `json:"duration"`
}

// LoadBenchmarkResults decodes a results.json file.
func LoadBenchmarkResults(path string) (*BenchmarkResults, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read results: %w", err)
	}
	var r BenchmarkResults
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("decode results %s: %w", path, err)
	}
	if len(r.Results) == 0 {
		return nil, fmt.Errorf("results %s: no commands", path)
	}
	return &r, nil
}

// MergeResults unions the published results with ours; per command and
// engine our entries override, details are merged the same way. The result
// keeps the published schema so the same viewer could render it.
func MergeResults(ours, published *BenchmarkResults) *BenchmarkResults {
	merged := &BenchmarkResults{
		Details: map[string][]string{},
		Results: map[string]map[string][]QueryResult{},
	}
	for engine, lines := range published.Details {
		merged.Details[engine] = lines
	}
	for engine, lines := range ours.Details {
		merged.Details[engine] = lines
	}
	for command, engines := range published.Results {
		merged.Results[command] = map[string][]QueryResult{}
		for engine, qrs := range engines {
			merged.Results[command][engine] = qrs
		}
	}
	for command, engines := range ours.Results {
		if merged.Results[command] == nil {
			merged.Results[command] = map[string][]QueryResult{}
		}
		for engine, qrs := range engines {
			merged.Results[command][engine] = qrs
		}
	}
	return merged
}

// EngineStats summarizes one engine over one command (optionally one tag):
// the published page computes AVERAGE/P50/P90/P99 over the per-query minima,
// so we replicate exactly that math.
type EngineStats struct {
	Engine  string
	Average float64 // µs, mean of per-query minima
	P50     float64 // µs
	P90     float64 // µs
	P99     float64 // µs
	Queries int     // queries that produced a duration
}

// CommandTagStats computes per-engine stats for one command, restricted to
// queries whose tags include tag ("" = all queries). Engines are returned in
// a deterministic order: alphabetical, seek last.
func CommandTagStats(r *BenchmarkResults, command, tag string) []EngineStats {
	engines := orderedEngines(r, command)
	stats := make([]EngineStats, 0, len(engines))
	for _, engine := range engines {
		minima := perQueryMinimaMicros(r.Results[command][engine], tag)
		stats = append(stats, EngineStats{
			Engine:  engine,
			Average: mean(minima),
			P50:     percentile(minima, 0.50),
			P90:     percentile(minima, 0.90),
			P99:     percentile(minima, 0.99),
			Queries: len(minima),
		})
	}
	return stats
}

// orderedEngines lists engines present under a command, alphabetically with
// seek last so the table reads published-engines-first.
func orderedEngines(r *BenchmarkResults, command string) []string {
	var engines []string
	hasSeek := false
	for engine := range r.Results[command] {
		if engine == SeekEngineName {
			hasSeek = true
			continue
		}
		engines = append(engines, engine)
	}
	sort.Strings(engines)
	if hasSeek {
		engines = append(engines, SeekEngineName)
	}
	return engines
}

// perQueryMinimaMicros reduces each query's duration array to its minimum
// (the published methodology keeps the best of 10 runs per query), skipping
// queries with empty arrays (unsupported), and optionally filtering by tag.
func perQueryMinimaMicros(qrs []QueryResult, tag string) []float64 {
	var minima []float64
	for _, qr := range qrs {
		if tag != "" && !containsTag(qr.Tags, tag) {
			continue
		}
		if len(qr.Duration) == 0 {
			continue
		}
		min := qr.Duration[0]
		for _, d := range qr.Duration[1:] {
			if d < min {
				min = d
			}
		}
		minima = append(minima, float64(min))
	}
	sort.Float64s(minima)
	return minima
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// percentile returns the nearest-rank percentile of an ascending slice.
func percentile(sortedAsc []float64, p float64) float64 {
	if len(sortedAsc) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sortedAsc)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sortedAsc) {
		idx = len(sortedAsc) - 1
	}
	return sortedAsc[idx]
}

// CountMismatch is one query where seek's hit count differs from the
// reference engine's.
type CountMismatch struct {
	Query     string
	Seek      int
	Reference int
}

// CountAgreement compares seek's per-query hit counts with the reference
// engine's for the count-bearing commands. Tokenizer differences (unicode61
// vs Lucene's StandardTokenizer) are expected to cause a small mismatch
// tail; the agreement percentage quantifies it instead of hiding it.
type CountAgreement struct {
	Command    string
	Reference  string
	Queries    int
	Compared   int // queries answered by both sides (UNSUPPORTED excluded)
	Matched    int
	Mismatches []CountMismatch
}

// CountAgreements computes hit-count agreement against referenceEngine for
// every count-bearing command both sides report.
func CountAgreements(ours, published *BenchmarkResults, referenceEngine string) []CountAgreement {
	var agreements []CountAgreement
	for _, command := range KnownCommands {
		seekResults, ok := ours.Results[command][SeekEngineName]
		if !ok {
			continue
		}
		referenceResults, ok := published.Results[command][referenceEngine]
		if !ok {
			continue
		}
		reference := make(map[string]int, len(referenceResults))
		for _, qr := range referenceResults {
			reference[qr.Query] = qr.Count
		}
		agreement := CountAgreement{Command: command, Reference: referenceEngine, Queries: len(seekResults)}
		for _, qr := range seekResults {
			if qr.Count < 0 {
				// UNSUPPORTED answers (count = -1, written by the patched
				// client.py) were never answered — they neither match nor
				// mismatch, so they must not deflate the agreement rate.
				continue
			}
			refCount, ok := reference[qr.Query]
			if !ok || refCount < 0 {
				continue
			}
			agreement.Compared++
			if qr.Count == refCount {
				agreement.Matched++
			} else if len(agreement.Mismatches) < 10 {
				agreement.Mismatches = append(agreement.Mismatches, CountMismatch{Query: qr.Query, Seek: qr.Count, Reference: refCount})
			}
		}
		agreements = append(agreements, agreement)
	}
	return agreements
}

// ReportOptions carries the provenance lines rendered into the markdown.
type ReportOptions struct {
	GeneratedAt     time.Time
	SeekVersion     string
	SeekMachine     string // hardware disclosure for the machine that ran seek
	PublishedSource string // where the published numbers came from
	Commands        []string
	TagCommands     []string // commands that get a per-tag P50 table
	Tags            []string // tags covered by the per-tag tables
	ReferenceEngine string   // published engine hit counts are checked against
}

// RenderMarkdown renders the merged benchmark tables as markdown for
// docs/benchmarks.md. Tables follow the published page's aggregation: every
// cell is derived from per-query minima, and the best engine per row is
// bolded.
func RenderMarkdown(merged *BenchmarkResults, opts ReportOptions) string {
	var b strings.Builder
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	commands := opts.Commands
	if len(commands) == 0 {
		commands = KnownCommands
	}
	tagCommands := opts.TagCommands
	if len(tagCommands) == 0 {
		tagCommands = []string{"COUNT", "TOP_100"}
	}
	tags := opts.Tags
	if len(tags) == 0 {
		tags = []string{"term", "intersection", "union", "phrase"}
	}

	fmt.Fprintf(&b, "# Search benchmark results\n\n")
	fmt.Fprintf(&b, "Generated: %s. seek participates in the [search-benchmark-game](https://github.com/quickwit-stuff/search-benchmark-game) harness on the same corpus and query set as the engines published on the [tantivy bench page](https://tantivy-search.github.io/bench/).\n\n", generatedAt.Format("2006-01-02"))

	b.WriteString(renderMethodology(opts))
	b.WriteString("\n## Overall latency (per-query minimum, µs)\n\n")
	for _, command := range commands {
		if len(merged.Results[command]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", command)
		b.WriteString(renderStatsTable(CommandTagStats(merged, command, "")))
		b.WriteString("\n")
	}

	b.WriteString("## Latency by query type (P50, per-query minimum, µs)\n\n")
	for _, command := range tagCommands {
		if len(merged.Results[command]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", command)
		b.WriteString(renderTagTables(merged, command, tags))
		b.WriteString("\n")
	}

	b.WriteString(renderCountAgreement(merged, opts))
	b.WriteString(renderLimitations())
	return b.String()
}

func renderMethodology(opts ReportOptions) string {
	var b strings.Builder
	b.WriteString("## Methodology\n\n")
	b.WriteString("- Corpus: English Wikipedia (`wiki-articles.json.bz2`), transformed by the harness (`corpus_transform.py`: lowercased, non-letter characters collapsed to spaces) and indexed as one document per article.\n")
	b.WriteString("- Queries: the harness's AOL-derived Lucene-syntax query set (term / intersection / union / phrase tags), run under five commands: TOP_10, TOP_100, TOP_1000, TOP_100_COUNT, COUNT.\n")
	b.WriteString("- Protocol: 10-minute warmup per command (`random.seed(2)` shuffle), then 10 measured passes; the per-query minimum of the 10 runs is what the tables aggregate (µs).\n")
	b.WriteString("- seek engine: SQLite FTS5 (`unicode61 remove_diacritics 2`, no stemming) over doc-level FTS rows, bm25 ranking with the standard FTS5 k1=1.2/b=0.75, real top-K retrieval through seek's search path.\n")
	if opts.SeekMachine != "" {
		fmt.Fprintf(&b, "- seek ran on: %s.\n", opts.SeekMachine)
	}
	fmt.Fprintf(&b, "- Published engines ran on dedicated AWS hardware (see the [harness README](https://github.com/quickwit-stuff/search-benchmark-game)); seek's numbers come from a different machine, so cross-engine rows indicate order-of-magnitude, not exact ranking. Seek version: %s.\n", opts.SeekVersion)
	if opts.PublishedSource != "" {
		fmt.Fprintf(&b, "- Published numbers source: %s.\n", opts.PublishedSource)
	}
	b.WriteString("\n")
	return b.String()
}

func renderStatsTable(stats []EngineStats) string {
	var b strings.Builder
	b.WriteString("| Metric |")
	for _, s := range stats {
		fmt.Fprintf(&b, " %s |", s.Engine)
	}
	b.WriteString("\n|---|")
	for range stats {
		b.WriteString("---|")
	}
	b.WriteString("\n")

	rows := []struct {
		label string
		value func(EngineStats) float64
	}{
		{"AVERAGE", func(s EngineStats) float64 { return s.Average }},
		{"P50", func(s EngineStats) float64 { return s.P50 }},
		{"P90", func(s EngineStats) float64 { return s.P90 }},
		{"P99", func(s EngineStats) float64 { return s.P99 }},
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "| %s |", row.label)
		best := bestValue(stats, row.value)
		for _, s := range stats {
			v := row.value(s)
			cell := "—"
			if s.Queries > 0 {
				cell = fmt.Sprintf("%.0f", v)
				if v == best {
					cell = "**" + cell + "**"
				}
			}
			fmt.Fprintf(&b, " %s |", cell)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\nQueries answered: ")
	for i, s := range stats {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s %d", s.Engine, s.Queries)
	}
	b.WriteString("\n")
	return b.String()
}

func renderTagTables(r *BenchmarkResults, command string, tags []string) string {
	var b strings.Builder
	b.WriteString("| Query type |")
	for _, engine := range orderedEngines(r, command) {
		fmt.Fprintf(&b, " %s |", engine)
	}
	b.WriteString("\n|---|")
	for range orderedEngines(r, command) {
		b.WriteString("---|")
	}
	b.WriteString("\n")

	statsByEngineTag := map[string]map[string]EngineStats{}
	for _, tag := range tags {
		for _, s := range CommandTagStats(r, command, tag) {
			if statsByEngineTag[s.Engine] == nil {
				statsByEngineTag[s.Engine] = map[string]EngineStats{}
			}
			statsByEngineTag[s.Engine][tag] = s
		}
	}
	for _, tag := range tags {
		fmt.Fprintf(&b, "| %s |", tag)
		var p50s []float64
		engines := orderedEngines(r, command)
		for _, engine := range engines {
			if s, ok := statsByEngineTag[engine][tag]; ok && s.Queries > 0 {
				p50s = append(p50s, s.P50)
			} else {
				p50s = append(p50s, -1)
			}
		}
		best := -1.0
		for _, v := range p50s {
			if v >= 0 && (best < 0 || v < best) {
				best = v
			}
		}
		for _, v := range p50s {
			cell := "—"
			if v >= 0 {
				cell = fmt.Sprintf("%.0f", v)
				if v == best {
					cell = "**" + cell + "**"
				}
			}
			fmt.Fprintf(&b, " %s |", cell)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderCountAgreement(merged *BenchmarkResults, opts ReportOptions) string {
	reference := opts.ReferenceEngine
	if reference == "" {
		reference = "tantivy-0.25"
	}
	var b strings.Builder
	b.WriteString("## Hit-count agreement vs reference engine\n\n")
	b.WriteString("seek's per-query hit counts are compared with the reference engine's published counts on the same corpus and queries. Agreement quantifies tokenizer differences (unicode61 vs Lucene's StandardTokenizer) instead of hiding them.\n\n")
	b.WriteString("| Command | Reference | Queries | Exact matches | Agreement |\n|---|---|---|---|---|\n")
	any := false
	for _, agreement := range CountAgreements(merged, merged, reference) {
		if agreement.Compared == 0 {
			continue
		}
		any = true
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %.1f%% |\n", agreement.Command, agreement.Reference, agreement.Compared, agreement.Matched, 100*float64(agreement.Matched)/float64(agreement.Compared))
	}
	if !any {
		b.WriteString("| — | — | 0 | 0 | — |\n")
	}
	b.WriteString("\n")
	return b.String()
}

func renderLimitations() string {
	return `## Known limitations of the seek engine column

- FTS5 has no lazy top-K pruning (no WAND/block-max tricks), so TOP_K commands score every match before ranking; engines with WAND keep an edge that grows with K.
- seek chunks documents for its own retrieval model; benchmark counts are document-level (the FTS index is doc-level), but chunk storage adds indexing cost that single-document engines do not pay.
- Shared-machine numbers vary run to run; rerun on the same machine before drawing fine-grained conclusions.

## Reproducing

See ` + "`third_party/search-benchmark-game/README.md`" + ` for the manual corpus download, indexing, and measurement steps (fully offline, no CI involved).
`
}

// bestValue returns the lowest finite value across stats for a metric, or
// +Inf when nothing measured.
func bestValue(stats []EngineStats, value func(EngineStats) float64) float64 {
	best := math.Inf(1)
	for _, s := range stats {
		if s.Queries > 0 {
			best = math.Min(best, value(s))
		}
	}
	return best
}
