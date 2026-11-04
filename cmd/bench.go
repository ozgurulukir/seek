package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ozgurulukir/seek/internal/bench"
	"github.com/ozgurulukir/seek/internal/buildinfo"
	"github.com/ozgurulukir/seek/internal/config"
)

// BenchCmd bridges seek into the search-benchmark-game harness
// (https://github.com/quickwit-stuff/search-benchmark-game) so seek can
// publish results alongside the engines on tantivy-search.github.io/bench.
// The harness drives `index` (corpus JSONL on stdin) and `serve` (query line
// protocol on stdin/stdout); `report` merges seek's results.json with the
// published numbers into docs/benchmarks.md.
type BenchCmd struct {
	Index  BenchIndexCmd  `cmd:"" help:"Index a search-benchmark-game corpus (JSONL {id,text} on stdin)"`
	Serve  BenchServeCmd  `cmd:"" help:"Serve the search-benchmark-game query protocol (stdin/stdout)"`
	Report BenchReportCmd `cmd:"" help:"Merge seek bench results with the published table into markdown"`
}

type BenchIndexCmd struct {
	DB         string `help:"Path to the benchmark SQLite database."`
	Collection string `help:"Collection name inside the benchmark database." default:"benchmark-game"`
	BatchSize  int    `help:"Documents committed per transaction." default:"500"`
}

func (c *BenchIndexCmd) Run(cfg *config.AppConfig) (err error) {
	dbPath := c.DB
	if dbPath == "" {
		dbPath = benchDBPath()
	}
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create bench dir: %w", err)
		}
	}

	progressed := false
	stats, err := bench.Ingest(context.Background(), os.Stdin, bench.IngestOptions{
		DBPath:         dbPath,
		CollectionName: c.Collection,
		BatchSize:      c.BatchSize,
	}, func(docs, bytes int64) {
		progressed = true
		fmt.Fprintf(os.Stderr, "\rindexed %d docs (%.1f MB)", docs, float64(bytes)/(1<<20))
	})
	if err != nil {
		return err
	}
	if progressed {
		fmt.Fprintf(os.Stderr, "\n")
	}
	secs := stats.Elapsed.Seconds()
	rate := 0.0
	if secs > 0 {
		rate = float64(stats.Docs) / secs
	}
	fmt.Printf("indexed %d docs (%.1f MB) in %s — %.0f docs/s\n", stats.Docs, float64(stats.Bytes)/(1<<20), stats.Elapsed.Round(1*time.Second), rate)
	if stats.Skipped > 0 {
		fmt.Printf("skipped %d docs without id\n", stats.Skipped)
	}
	return nil
}

type BenchServeCmd struct {
	DB string `help:"Path to the benchmark SQLite database."`
}

func (c *BenchServeCmd) Run(cfg *config.AppConfig) error {
	dbPath := c.DB
	if dbPath == "" {
		dbPath = benchDBPath()
	}
	return bench.Serve(context.Background(), os.Stdin, os.Stdout, bench.ServeOptions{DBPath: dbPath})
}

type BenchReportCmd struct {
	Results     string `help:"Path to seek's harness results.json."`
	Published   string `help:"Path to the published results.json fetched from tantivy-search.github.io."`
	EmitQueries string `help:"Extract the harness query set from --results into this queries.txt and exit."`
	Out         string `help:"Markdown report output path." default:"docs/benchmarks.md"`
	Merged      string `help:"Merged results.json output path." default:"docs/bench/results.json"`
	Machine     string `help:"Hardware disclosure line (defaults to a generic CPU/OS description)."`
	Reference   string `help:"Published engine used as the hit-count reference." default:"tantivy-0.25"`
}

func (c *BenchReportCmd) Run(cfg *config.AppConfig) (err error) {
	if c.EmitQueries != "" {
		resultsPath := c.Results
		if resultsPath == "" {
			resultsPath = filepath.Join(benchDir(), "results.json")
		}
		return bench.EmitQueries(resultsPath, c.EmitQueries)
	}

	resultsPath := c.Results
	if resultsPath == "" {
		resultsPath = filepath.Join(benchDir(), "results.json")
	}
	publishedPath := c.Published
	if publishedPath == "" {
		publishedPath = filepath.Join(benchDir(), "results-published.json")
	}

	ours, err := bench.LoadBenchmarkResults(resultsPath)
	if err != nil {
		return err
	}
	published, err := bench.LoadBenchmarkResults(publishedPath)
	if err != nil {
		return err
	}
	merged := bench.MergeResults(ours, published)

	machine := c.Machine
	if machine == "" {
		machine = fmt.Sprintf("%d CPUs, %s/%s (shared machine)", runtime.NumCPU(), runtime.GOOS, runtime.GOARCH)
	}

	if dir := filepath.Dir(c.Merged); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create merged results dir: %w", err)
		}
	}
	mergedJSON, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("encode merged results: %w", err)
	}
	if err := os.WriteFile(c.Merged, mergedJSON, 0o644); err != nil {
		return fmt.Errorf("write merged results: %w", err)
	}

	md := bench.RenderMarkdown(merged, bench.ReportOptions{
		GeneratedAt:     time.Now(),
		SeekVersion:     buildinfo.Version,
		SeekMachine:     machine,
		PublishedSource: "https://tantivy-search.github.io/bench/results.json",
		ReferenceEngine: c.Reference,
	})
	if dir := filepath.Dir(c.Out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create report dir: %w", err)
		}
	}
	if err := os.WriteFile(c.Out, []byte(md), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	fmt.Printf("wrote %s and %s\n", c.Merged, c.Out)
	return nil
}

// benchDir is the default home of benchmark artifacts (corpus, index DB,
// results) inside seek's cache directory.
func benchDir() string {
	return filepath.Join(config.CacheDir(), "bench")
}

func benchDBPath() string {
	return filepath.Join(benchDir(), "index.db")
}
