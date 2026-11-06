# Search benchmark results

Generated: 2026-11-06. seek participates in the [search-benchmark-game](https://github.com/quickwit-stuff/search-benchmark-game) harness on the same corpus and query set as the engines published on the [tantivy bench page](https://tantivy-search.github.io/bench/). Every engine column in this report was measured on one machine in one harness run, so the columns are directly comparable; published numbers from different hardware are intentionally not mixed in.

## Methodology

- Corpus: English Wikipedia (`wiki-articles.json.bz2`), transformed by the harness (`corpus_transform.py`: lowercased, non-letter characters collapsed to spaces) and indexed as one document per article.
- Queries: the harness's AOL-derived Lucene-syntax query set (term / intersection / union / phrase tags), run under five commands: TOP_10, TOP_100, TOP_1000, TOP_100_COUNT, COUNT.
- Protocol: 10-minute warmup per command (`random.seed(2)` shuffle), then 10 measured passes; the per-query minimum of the 10 runs is what the tables aggregate (µs).
- seek engine: SQLite FTS5 (`unicode61 remove_diacritics 2`, no stemming) over doc-level FTS rows, bm25 ranking with the standard FTS5 k1=1.2/b=0.75, real top-K retrieval through seek's search path.
- All engine columns ran on this machine, measured by the same harness in the same run: Intel(R) Core(TM) i7-4770K CPU @ 3.50GHz, 8 cores, 7.4Gi RAM, xanmod kernel.
- Seek version: dev. Reference engines are fetched from the pinned upstream commit via `make fetch-engines` (see the [harness README](https://github.com/quickwit-stuff/search-benchmark-game)).


## Overall latency (per-query minimum, µs)

### TOP_10

| Metric | tantivy-0.22 | seek |
|---|---|---|
| AVERAGE | **2226** | 352155 |
| P50 | **713** | 31917 |
| P90 | **3712** | 623726 |
| P99 | **16497** | 5217666 |

Queries answered: tantivy-0.22 962, seek 962

### TOP_100

| Metric | tantivy-0.22 | seek |
|---|---|---|
| AVERAGE | **2580** | 377362 |
| P50 | **868** | 51096 |
| P90 | **4663** | 668212 |
| P99 | **20451** | 5308832 |

Queries answered: tantivy-0.22 962, seek 962

### TOP_1000

| Metric | tantivy-0.22 | seek |
|---|---|---|
| AVERAGE | **3360** | 619398 |
| P50 | **1217** | 311531 |
| P90 | **7546** | 1107773 |
| P99 | **24249** | 7097769 |

Queries answered: tantivy-0.22 962, seek 962

### TOP_100_COUNT

| Metric | tantivy-0.22 | seek |
|---|---|---|
| AVERAGE | **4408** | 497203 |
| P50 | **1091** | 62018 |
| P90 | **7830** | 992589 |
| P99 | **59984** | 6418770 |

Queries answered: tantivy-0.22 962, seek 962

### COUNT

| Metric | tantivy-0.22 | seek |
|---|---|---|
| AVERAGE | **1828** | 109057 |
| P50 | **450** | 12380 |
| P90 | **2846** | 320537 |
| P99 | **18045** | 992110 |

Queries answered: tantivy-0.22 962, seek 962

## Latency by query type (P50, per-query minimum, µs)

### COUNT

| Query type | tantivy-0.22 | seek |
|---|---|---|
| term | **11** | 859741 |
| intersection | **424** | 5849 |
| union | **415** | 197645 |
| phrase | **642** | 3510 |

### TOP_100

| Query type | tantivy-0.22 | seek |
|---|---|---|
| term | **2904** | 5129670 |
| intersection | **536** | 40448 |
| union | **1307** | 370173 |
| phrase | **669** | 13561 |

## Hit-count agreement vs reference engine

seek's per-query hit counts are compared with the reference engine's counts, both measured in the same run on the same machine. Agreement quantifies tokenizer differences (unicode61 vs Lucene's StandardAnalyzer) instead of hiding them.

| Command | Reference | Queries | Exact matches | Agreement |
|---|---|---|---|---|
| TOP_10 | tantivy-0.22 | 962 | 962 | 100.0% |
| TOP_100 | tantivy-0.22 | 962 | 962 | 100.0% |
| TOP_1000 | tantivy-0.22 | 962 | 962 | 100.0% |
| TOP_100_COUNT | tantivy-0.22 | 962 | 962 | 100.0% |
| COUNT | tantivy-0.22 | 962 | 962 | 100.0% |

## Known limitations of the seek engine column

- FTS5 has no lazy top-K pruning (no WAND/block-max tricks), so TOP_K commands score every match before ranking; engines with WAND keep an edge that grows with K.
- seek chunks documents for its own retrieval model; benchmark counts are document-level (the FTS index is doc-level), but chunk storage adds indexing cost that single-document engines do not pay.
- Shared-machine numbers vary run to run; rerun on the same machine before drawing fine-grained conclusions.

## Reproducing

See `third_party/search-benchmark-game/README.md` for the manual corpus download, indexing, and measurement steps (fully offline, no CI involved).
