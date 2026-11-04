# Search benchmark results

> **Status: awaiting the first full run.** The pipeline is implemented and
> smoke-tested (`make -C third_party/search-benchmark-game smoke`); the numbers in
> this file are filled by `seek bench report` once the full Wikipedia corpus
> has been indexed and measured on the target machine. The measurement
> procedure is identical to the published
> [tantivy bench page](https://tantivy-search.github.io/bench/) — same
> harness ([search-benchmark-game](https://github.com/quickwit-stuff/search-benchmark-game)),
> same English Wikipedia corpus, same query set.

## Methodology

- Corpus: English Wikipedia (`wiki-articles.json.bz2`), transformed by the harness (`corpus_transform.py`: lowercased, non-letter characters collapsed to spaces) and indexed as one document per article.
- Queries: the harness's AOL-derived Lucene-syntax query set (term / intersection / union / phrase tags), run under five commands: TOP_10, TOP_100, TOP_1000, TOP_100_COUNT, COUNT.
- Protocol: 10-minute warmup per command (`random.seed(2)` shuffle), then 10 measured passes; the per-query minimum of the 10 runs is what the tables aggregate (µs).
- seek engine: SQLite FTS5 (`unicode61 remove_diacritics 2`, no stemming) over doc-level FTS rows, bm25 ranking with the standard FTS5 k1=1.2/b=0.75, real top-K retrieval through seek's search path.
- Published engines ran on dedicated AWS hardware (see the [harness README](https://github.com/quickwit-stuff/search-benchmark-game)); seek's numbers come from a different machine, so cross-engine rows indicate order-of-magnitude, not exact ranking.
- Published numbers source: https://tantivy-search.github.io/bench/results.json

## Overall latency (per-query minimum, µs)

Awaiting the first full run — regenerate with:

```sh
bin/seek bench report \
  --results third_party/search-benchmark-game/results.json \
  --published third_party/search-benchmark-game/results-published.json \
  --machine "<CPU model, cores, RAM, OS>"
```

## Latency by query type (P50, per-query minimum, µs)

Awaiting the first full run.

## Hit-count agreement vs reference engine

Awaiting the first full run.

## Known limitations of the seek engine column

- FTS5 has no lazy top-K pruning (no WAND/block-max tricks), so TOP_K commands score every match before ranking; engines with WAND keep an edge that grows with K.
- seek chunks documents for its own retrieval model; benchmark counts are document-level (the FTS index is doc-level), but chunk storage adds indexing cost that single-document engines do not pay.
- Shared-machine numbers vary run to run; rerun on the same machine before drawing fine-grained conclusions.

## Reproducing

See [third_party/search-benchmark-game/README.md](../third_party/search-benchmark-game/README.md) for the manual corpus download, indexing, and measurement steps (fully manual, no CI involved).
