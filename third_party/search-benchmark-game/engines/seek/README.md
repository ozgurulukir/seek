# seek engine

seek as a search-benchmark-game engine: a Go hybrid (BM25 + vector) search
engine whose SQLite FTS5 path participates in the BM25 commands of the
benchmark. This is the engine behind the `seek` column in
`docs/benchmarks.md`.

## Contract implementation

- `make index` streams the harness corpus (`$CORPUS`) through
  `seek bench index --db index/bench.db` — full body into the doc-level FTS
  table, body chunked by seek's default chunker into chunk rows (that chunk
  storage is part of seek's real indexing cost and is included).
- `make serve` runs `seek bench serve --db index/bench.db` — the line
  protocol answers COUNT / TOP_K / TOP_K_COUNT per CONTRIBUTE.md. TOP_K
  actually computes the ranked top-K (and outputs `1` per the contract);
  TOP_K_COUNT additionally returns the total matching document count.
- `make clean` removes `index/`.

## Documented deviations from Lucene parity

- Tokenization: SQLite FTS5 `unicode61` tokenizer with
  `remove_diacritics 2`; no stemming (per contract). The corpus is
  pre-normalized by `corpus_transform.py` (lowercase, non-letters collapsed
  to single spaces), so unicode61 ≈ StandardTokenizer for this corpus;
  a small count-agreement tail vs Lucene engines remains (see the
  "Hit-count agreement" section of `docs/benchmarks.md`).
- BM25: FTS5's built-in `bm25()` uses k1=1.2, b=0.75 (same defaults as
  Lucene); the FTS title column is empty because the corpus has no titles,
  so seek's 10x title boost never applies here.
- Query translation: the game's Lucene subset is translated per-query by
  `internal/bench` (`GameQueryToFTS5`): intersection (`+a +b`) → AND,
  union (`a b`) → OR (Lucene's default operator), phrases → FTS5 phrases,
  negation (`-a`) → FTS5 NOT applied to the positive match set in query
  order (Lucene MUST_NOT; verified 19/19 exact count match against the
  published tantivy-0.25 numbers on the full corpus), mixed queries → AND
  of required clauses (optional clauses never restrict Lucene's match
  set).
- No WAND/lazy top-K: FTS5 scores every matching document for TOP_K before
  ranking, so TOP_1000 costs scale with match frequency rather than K.

## Notes

- The engine keeps its index in `index/` (WAL mode), built once per corpus
  and reused across measurement runs — same as every other engine here.
- Build seek first: `make build` at the repo root produces `bin/seek`; the
  harness Makefile exports `SEEK=$(repo)/bin/seek`.
