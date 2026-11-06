# search-benchmark-game (vendored for seek)

A vendored fork of
[quickwit-stuff/search-benchmark-game](https://github.com/quickwit-stuff/search-benchmark-game)
(MIT, pinned at `3c124e2823e27e2c425769ffac24c71085065502`) — the Python
harness that produces the numbers on
[tantivy-search.github.io/bench](https://tantivy-search.github.io/bench/).
seek ships an engine for it (`engines/seek/`, backed by `seek bench index`
and `seek bench serve`), so seek's BM25 numbers are produced by the same
harness, on the same corpus and the same queries as the published engines,
and land in `docs/benchmarks.md` via `seek bench report`.

Other engines (tantivy, Lucene, pisa, ...) are **not** vendored; their
numbers come from the published `results.json`.

## Layout

- `src/client.py` — upstream harness (patched, see below). Runs each engine
  as a long-lived subprocess via `make serve`, 10-minute warmup per command
  (`random.seed(2)` shuffle), 10 measured passes, writes `results.json`
  (`{details, results:{COMMAND:{engine:[{query,tags,count,duration[µs]}]}}}`).
- `corpus_transform.py` — upstream, verbatim. Transforms
  `wiki-articles.json.bz2` (lowercase, non-letters → spaces) into corpus
  JSONL `{"id","text"}`.
- `CONTRIBUTE.md` — upstream engine contract (verbatim, for reference).
- `LICENSE` — upstream MIT.
- `engines/seek/` — the seek engine (see its README for documented
  deviations from Lucene parity).
- `Makefile` — adapted for seek: `SEEK`/`ENGINES`/`COMMANDS` variables,
  `fetch-published`/`queries` targets, and a fixture `smoke` target.

## Patches over upstream

1. `WARMUP_TIME` / `NUM_ITER` are env-overridable (upstream hardcodes 10
   minutes / 10 passes) so the `smoke` target can run in seconds.
2. Engine names and CLI paths are validated/normalized (`validate_engine`,
   `engine_dir` confines engine resolution inside `engines/`;
   `safe_input_path` normalizes input paths) — hardening added in this fork.
3. The UNSUPPORTED branch writes `{"count": -1, "duration": []}` with string
   keys (upstream wrote `{count: -1, duration: []}` with variable keys — a
   latent bug that made the entry unreachable nonsense; output was unchanged
   because `engine_results` keeps the original dicts).

## Manual benchmark workflow (fully offline from CI, like upstream)

```sh
make build                                    # repo root → bin/seek

cd third_party/search-benchmark-game

make corpus            # ~20 GB download + transform → corpus.json (6.5M docs)
make index             # streams corpus.json through `seek bench index`
                       # (~1-3 h, index at engines/seek/index/bench.db)
make fetch-published   # published results.json → results-published.json
make queries           # extract the 962-query set → queries.txt
make bench             # 5 commands × (10 min warmup + 10 passes); hours; → results.json
```

Then build the docs:

```sh
cd ../..
bin/seek bench report \
  --results third_party/search-benchmark-game/results.json \
  --published third_party/search-benchmark-game/results-published.json \
  --machine "CPU model, N cores, RAM, OS"   # hardware disclosure line
# writes docs/bench/results.json (merged raw arrays) and docs/benchmarks.md
```

`queries.txt` is derived from the published `results.json` (`seek bench
report --emit-queries`), not from upstream's stale `queries.txt`, so every
seek query has a published counterpart to compare hit counts against.

## Disk / time expectations

- `corpus.json` ~20 GB; the seek index (SQLite, WAL) is roughly 1.5-2× the
  text size. Keep ~70-90 GB free for a full run.
- `make bench` measures 5 commands × 962 queries with a 10-minute warmup per
  command — plan for several hours per engine.

## If the Dropbox corpus link is dead

`WIKI_SRC` in the Makefile points at the harness's original
`wiki-articles.json.bz2`. If it rots, re-derive an equivalent corpus from
Wikimedia dumps with the same `corpus_transform.py` transform — but note
that hit counts will then differ from the published table, so the run is no
longer count-comparable (disclose it in docs/benchmarks.md).

## Running other engines locally (same-hardware reference)

`make fetch-engines` downloads external engine sources from the pinned
upstream commit (sparse git clone; gitignored, never vendored) so they can be
indexed and benched on the same machine as seek:

```sh
make fetch-engines                                        # default: tantivy-0.22
make index ENGINES="tantivy-0.22"                         # builds + indexes (~10-30 min)
make bench ENGINES="tantivy-0.22 seek"                    # measures both in one run
```

Engine verification (fixture corpus, hand-checked counts): **tantivy-0.22**
implements the full protocol correctly — its COUNT/TOP_K/phrase answers match
seek's exactly. The other upstream engines are NOT suitable for the published
comparison: **bleve-0.8.0-scorch** answers all commands but returns 0 for
phrase queries (its `do_query.go` uses a raw QueryStringQuery), and
**bluge-0.2.2** supports only TOP_10/TOP_10_COUNT (everything else answers
UNSUPPORTED). They remain fetchable via
`make fetch-engines EXTERNAL_ENGINES="bleve-0.8.0-scorch ..."` for
experimentation.

## Smoke test

`make smoke` runs the whole pipeline (index → harness → results.json shape
check) against `testdata/smoke-corpus.jsonl` (15 fixture docs) with a tiny
warmup; it must print `smoke OK: {'TOP_10': 11, 'COUNT': 11}`.
