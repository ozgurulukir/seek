# Search benchmark results

Generated: 2026-11-05. seek participates in the [search-benchmark-game](https://github.com/quickwit-stuff/search-benchmark-game) harness on the same corpus and query set as the engines published on the [tantivy bench page](https://tantivy-search.github.io/bench/).

## Methodology

- Corpus: English Wikipedia (`wiki-articles.json.bz2`), transformed by the harness (`corpus_transform.py`: lowercased, non-letter characters collapsed to spaces) and indexed as one document per article.
- Queries: the harness's AOL-derived Lucene-syntax query set (term / intersection / union / phrase tags), run under five commands: TOP_10, TOP_100, TOP_1000, TOP_100_COUNT, COUNT.
- Protocol: 10-minute warmup per command (`random.seed(2)` shuffle), then 10 measured passes; the per-query minimum of the 10 runs is what the tables aggregate (µs).
- seek engine: SQLite FTS5 (`unicode61 remove_diacritics 2`, no stemming) over doc-level FTS rows, bm25 ranking with the standard FTS5 k1=1.2/b=0.75, real top-K retrieval through seek's search path.
- seek ran on: Intel(R) Core(TM) i7-4770K CPU @ 3.50GHz, 8 çekirdek, 7.4Gi RAM, 7.3.1-x64v3-xanmod1.
- Published engines ran on dedicated AWS hardware (see the [harness README](https://github.com/quickwit-stuff/search-benchmark-game)); seek's numbers come from a different machine, so cross-engine rows indicate order-of-magnitude, not exact ranking. Seek version: dev.
- Published numbers source: https://tantivy-search.github.io/bench/results.json.


## Overall latency (per-query minimum, µs)

### TOP_10

| Metric | iresearch-26.03.1 | lucene-10.3.0 | lucene-10.3.0-bp | pisa-0.8.2 | tantivy-0.25 | seek |
|---|---|---|---|---|---|---|
| AVERAGE | 558 | 626 | **417** | 462 | 980 | 361526 |
| P50 | 216 | 228 | 179 | **167** | 309 | 32516 |
| P90 | 1050 | 1032 | **692** | 758 | 1545 | 657630 |
| P99 | 6511 | 5268 | **3132** | 3657 | 8617 | 5176184 |

Queries answered: iresearch-26.03.1 962, lucene-10.3.0 962, lucene-10.3.0-bp 962, pisa-0.8.2 661, tantivy-0.25 962, seek 943

### TOP_100

| Metric | iresearch-26.03.1 | lucene-10.3.0 | lucene-10.3.0-bp | pisa-0.8.2 | tantivy-0.25 | seek |
|---|---|---|---|---|---|---|
| AVERAGE | 636 | 873 | 666 | **600** | 1136 | 374089 |
| P50 | 268 | 353 | 259 | **207** | 371 | 50431 |
| P90 | 1216 | 1485 | **1033** | 1080 | 1847 | 658960 |
| P99 | 6864 | 6795 | **4724** | 6557 | 9579 | 5186991 |

Queries answered: iresearch-26.03.1 962, lucene-10.3.0 962, lucene-10.3.0-bp 962, pisa-0.8.2 661, tantivy-0.25 962, seek 943

### TOP_1000

| Metric | iresearch-26.03.1 | lucene-10.3.0 | lucene-10.3.0-bp | pisa-0.8.2 | tantivy-0.25 | seek |
|---|---|---|---|---|---|---|
| AVERAGE | 812 | 1269 | 988 | **770** | 1459 | 615966 |
| P50 | 410 | 683 | 468 | **236** | 535 | 304512 |
| P90 | 1802 | 2473 | 2114 | **1488** | 3063 | 1117339 |
| P99 | **6852** | 8650 | 7103 | 9824 | 11591 | 7048106 |

Queries answered: iresearch-26.03.1 962, lucene-10.3.0 962, lucene-10.3.0-bp 962, pisa-0.8.2 661, tantivy-0.25 962, seek 943

### TOP_100_COUNT

| Metric | iresearch-26.03.1 | lucene-10.3.0 | lucene-10.3.0-bp | pisa-0.8.2 | tantivy-0.25 | seek |
|---|---|---|---|---|---|---|
| AVERAGE | **879** | 2336 | 2251 | 1497 | 1729 | 510384 |
| P50 | 372 | 569 | 423 | **246** | 498 | 86861 |
| P90 | **1697** | 4275 | 3826 | 3314 | 3006 | 1032718 |
| P99 | **9509** | 29215 | 31281 | 21698 | 20432 | 6224719 |

Queries answered: iresearch-26.03.1 962, lucene-10.3.0 962, lucene-10.3.0-bp 962, pisa-0.8.2 661, tantivy-0.25 962, seek 943

### COUNT

| Metric | iresearch-26.03.1 | lucene-10.3.0 | lucene-10.3.0-bp | pisa-0.8.2 | tantivy-0.25 | seek |
|---|---|---|---|---|---|---|
| AVERAGE | **450** | 747 | 591 | 1470 | 875 | 110896 |
| P50 | **169** | 234 | 173 | 232 | 197 | 12329 |
| P90 | **796** | 1178 | 1061 | 3092 | 1395 | 332449 |
| P99 | **3456** | 7042 | 5762 | 19387 | 8377 | 984028 |

Queries answered: iresearch-26.03.1 962, lucene-10.3.0 962, lucene-10.3.0-bp 962, pisa-0.8.2 661, tantivy-0.25 962, seek 943

## Latency by query type (P50, per-query minimum, µs)

### COUNT

| Query type | iresearch-26.03.1 | lucene-10.3.0 | lucene-10.3.0-bp | pisa-0.8.2 | tantivy-0.25 | seek |
|---|---|---|---|---|---|---|
| term | **5** | 7 | 6 | **5** | **5** | 855974 |
| intersection | 132 | 193 | **125** | 131 | 191 | 5829 |
| union | 178 | 226 | 173 | 514 | **163** | 197467 |
| phrase | **253** | 388 | 291 | — | 303 | 3473 |

### TOP_100

| Query type | iresearch-26.03.1 | lucene-10.3.0 | lucene-10.3.0-bp | pisa-0.8.2 | tantivy-0.25 | seek |
|---|---|---|---|---|---|---|
| term | **337** | 2143 | 1580 | 31376 | 856 | 4999220 |
| intersection | **153** | 256 | 168 | **153** | 241 | 39098 |
| union | 362 | 392 | 308 | **259** | 495 | 356164 |
| phrase | **253** | 403 | 289 | — | 328 | 13328 |

## Hit-count agreement vs reference engine

seek's per-query hit counts are compared with the reference engine's published counts on the same corpus and queries. Agreement quantifies tokenizer differences (unicode61 vs Lucene's StandardTokenizer) instead of hiding them.

| Command | Reference | Queries | Exact matches | Agreement |
|---|---|---|---|---|
| TOP_10 | tantivy-0.25 | 962 | 943 | 98.0% |
| TOP_100 | tantivy-0.25 | 962 | 943 | 98.0% |
| TOP_1000 | tantivy-0.25 | 962 | 943 | 98.0% |
| TOP_100_COUNT | tantivy-0.25 | 962 | 943 | 98.0% |
| COUNT | tantivy-0.25 | 962 | 943 | 98.0% |

## Known limitations of the seek engine column

- FTS5 has no lazy top-K pruning (no WAND/block-max tricks), so TOP_K commands score every match before ranking; engines with WAND keep an edge that grows with K.
- seek chunks documents for its own retrieval model; benchmark counts are document-level (the FTS index is doc-level), but chunk storage adds indexing cost that single-document engines do not pay.
- Shared-machine numbers vary run to run; rerun on the same machine before drawing fine-grained conclusions.

## Reproducing

See `third_party/search-benchmark-game/README.md` for the manual corpus download, indexing, and measurement steps (fully offline, no CI involved).
