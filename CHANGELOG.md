# Changelog

All notable changes to `seek` are documented here. This follows
[Keep a Changelog](https://keepachangelog.com/) conventions.

## [0.6.1] - 2026-10-09

### Fixed

- **Keyword search dropped English words whose Porter stem is not a prefix** (issue #98): in the default (`parsed`) query mode the analyzer expanded every differing stem into a `stem*` FTS5 prefix. The index is unstemmed (`unicode61`), so a same-length rewrite such as `body`→`bodi` became the unmatchable `bodi*`, silently returning no results for `body`, `city`, `study`, and their inflections. `AnalyzeForQuery` now only expands when the stem is a genuine prefix of the surface token (or shorter, preserving Turkish root reconstruction like `kitabı`→`kitap`); otherwise it falls back to the surface token.
- **`seek sync --json` embedding outcome**: the embedding stage is classified before the per-file-failure check, so a run that embedded (or deliberately skipped) chunks is no longer reported as `embeddings: "not_run"`.
- **`seek_capabilities` agrees with the sync report**: a configured provider whose endpoint the `privacy.offline_only` policy refuses to reach is treated as unavailable.
- **`seek sync <collection> --path`**: restored the `%w` error chain, stopped printing the "validated inside collection" summary when the path guard rejected the run, and kept the empty-index hint off filtered invocations.
- **MCP `seek_search`**: tolerates empty-string list arguments (`collections`, `fields`, …) emitted by some clients instead of failing, and honors `sort_by`/`sort_order` — `_score` maps to relevance order and rejects a non-descending `sort_order`.
- **Semantic service startup** (issue #95): `NUMBA_DISABLE_JIT=1` is exported (via `setdefault`, so a user-set value wins) before importing `sentence_transformers`, avoiding a 10+ minute numba JIT warm-up; the tagger uses PCA, not UMAP, at runtime, so no hot path regresses. `tools/` and the bundled skill mirror are updated together.

### Performance

- **Agent hook target matching** (`internal/agenthooks`) memoizes the compiled target-hook regexes instead of rebuilding them per invocation, with a benchmark guarding the cache.

### Tests

- New coverage: the issue #98 analyzer unit tests and parsed-mode end-to-end regression test, MCP argument-parsing tests, sync-outcome tests, and a schema-driven parser availability test.

## [0.6.0] - 2026-10-05

### Added

- **Dormant config keys are now honored:** `search.default_limit` (default result limit for `seek search` and the MCP `seek_search` tool), `filters.enabled` + `filters.default_collection` (default collection filter when a request names no collection), `aggregations.enabled` (gates `--aggs` / MCP `aggs`), and `rerank.top_n` (caps the candidate pool sent to the cross-encoder). Previously every one of these was parsed from config.yaml but never consumed.
- **`filters.default_collection` validates:** an unknown collection name errors with `seek collection list` guidance instead of silently returning zero results for every search.
- **Extraction warnings:** per-page OCR failures, embedded-text extraction failures, and textless scanned pages without OCR enabled surface as counted `WARN` lines in the sync summary instead of silently indexing empty pages.

### Changed

- **`vector_index.hnsw.dimension` removed:** the HNSW index dimension always followed `embedding.dimensions`; the silent no-op key (and its default constant) is gone.
- **`rerank.top_n` is uncapped when unset:** the loader no longer forces a default of 10; `0` means rerank the full candidate pool, and `seek auth status` prints `top_n: auto`.

### Fixed

- **Built-in xberg endpoint default:** `DefaultXbergBaseURL` pointed at port 8000 (flashrank_server); it now matches the xberg server's actual bind port 8001, so `extractor.backend: xberg` with default config no longer sends extract requests to the wrong local service.

### Documentation

- Fact-check pass across README, AGENTS.md, `docs/`, and the bundled skill: Go version and LOC counts, the background service description, `seek status`/`seek rm` described as standalone commands, the missing `hermes` parser schema row, codex's `archived_sessions` path, `lz4` in the compression list, and a stale `-l` reference in `docs/local-setup.md`. `plugin.json` now tracks the release version.

## [0.5.12] - 2026-09-30

### Fixed

- **launchd service install (macOS):** `seek service start` no longer hands a possibly truncated plist to `launchctl bootstrap` — a failed flush on file close now surfaces as an error instead of silently installing a broken service definition. The unused `launchctl print` output variable in `seek service status` is also gone.
- **Windows checkouts of the skill service scripts:** the mirrored `tools/` ↔ `skills/seek/scripts/services/` trees are pinned `eol=lf` in `.gitattributes`, so `core.autocrlf=true` checkouts can no longer rewrite `setup.sh` to CRLF and break it.

### Refactored

- **`syncConversation` decomposition (`internal/indexer`):** the ~200-line shared Claude/Codex sync path is split into named helpers — `conversationTitle`, `buildConversationChunks`, `pruneEmptyConversationBatch`, `conversationBaseSeq`, and `writeConversationDocument` — with identical WARN text, counters, and error returns. Contracts are pinned by unit tests against a real temp SQLite store.

### Tests

- **Package layering guard (`layering_test.go`):** builds the real import graph via `go list -json` and fails on import cycles, layer-direction violations (internal must not import cmd/root, cmd must not import root), and `third_party/renameio` reaching back into the main module — the architecture documented in AGENTS.md is now enforced on every test run.
- **Skill-bundle SSOT guard (`TestSkillServiceBundleIsSynchronized`):** parses its file list from `scripts/sync-skill-services.py` (single curated list), byte-compares every canonical/bundled pair, and walks both trees so mirror hand-edits, unlisted files, and undeclared `tools/` directories fail with remediation hints. The sync script's `--check` compares CRLF-normalized bytes so its verdict agrees with the Go test on Windows checkouts.

## [0.5.11] - 2026-09-27

### Changed

- **Batched orphan-cleanup deletes:** `DeleteOrphansContext` no longer issues four DELETE statements per stale document. Deletes are grouped into `IN`-clause batches of 500, and a full-collection purge (`nil` livePaths, the reindex path) uses set-based subquery deletes against the collection ID directly. Behavior is pinned by a new unit test (partial purge keeps live documents and their FTS/fast-field projections; full purge empties the collection; repeat purges are no-ops) and covered by benchmarks.
- **Batched chunk-embedding persistence:** embedding passes now persist each API batch of chunk embeddings in a single SQLite transaction instead of one transaction per chunk. The live vector index is updated incrementally for new chunk IDs and rebuilt atomically (off to the side, then published) when existing IDs are replaced — the restore-previous-embedding-on-rebuild-failure safety net from the per-chunk path is preserved, including reporting restore failures.

### Refactored

- **`seek auth login`:** the interactive prompts (provider selection, custom provider details, API key entry, multimodal opt-in) are extracted into standalone functions so `AuthLoginCmd.Run` reads as a short sequence. Behavior unchanged, including the 1024-dimension default for custom providers.

### Tests

- New coverage pinning: `TaskPrefixes` case-insensitive model matching and explicit document-prefix override, `quoteQualifiedIdentifier` quoting (embedded quotes, multi-part names, empty identifier), the default configuration constants, and `SchemaRegistry.DefaultSchema` (zero-value registry, full field definition set, call-to-call consistency).

## [0.5.10] - 2026-09-26

### Added

- **Packaged optional Python services with the seek skill:** the semantic tagger, embedding server, and flashrank reranker now ship inside the `seek` skill/plugin package (marketplace manifests, per-platform setup scripts, and a CI workflow keeping the packaged copies in sync), so agents can bring up the optional NLP endpoints without cloning the repo.

### Fixed

Second multi-agent code-review verification pass — claims re-verified against HEAD, each fix landed with a regression test:

- **Agent hooks:** a settings file containing the literal JSON `null` no longer panics hook install/repair (nil map); it is treated as an empty writable settings map.
- **Windows service:** the Task Scheduler task is registered as a quoted argv (`"<seek.exe>" sync`) launched via CreateProcess instead of routing through `cmd.exe /c`, removing `%VAR%` expansion and quote-injection exposure from the binary path.
- **Semantic enrichment:** a transient semantic `/health` failure no longer persists an empty-capability fingerprint basis, which re-selected the whole collection for re-enrichment (twice with a half-up service). Backfill degrades to a warn + no-op pass; sync records nothing and warns once.
- **Vector index:** cancelling an embedding-space reset (Ctrl-C) can no longer leave the live HNSW index empty for the rest of the process; the restore now runs cancellation-proof and the empty graph is never persisted.
- **Indexer:** the per-operation logger swap (`WithLogger`) is guarded by the instance mutex like every other shared field.
- **xberg extractor:** extraction responses are capped at 64 MiB instead of buffering an unbounded remote body.
- **MCP:** the MCP server reports the real release version (via `internal/buildinfo`, ldflags-injected) instead of a hardcoded `"dev"`.
- **Search:** unknown field-scoped query terms (`tags:go`) now fail at parse time with an actionable error (`supported: title, content`) instead of an opaque FTS5 `no such column` — or silently degrading the hybrid BM25 leg; `title:`/`content:` queries are unchanged.
- **Docs:** corrected the stale full-vector-sync description in AGENTS.md.

## [0.5.9] - 2026-09-25

### Fixed

- **Chunking UTF-8 safety:** overlap truncation (`tailBytes`), snippet generation, and string truncation helpers now snap to rune boundaries, avoiding broken UTF-8 byte sequences.
- **Indexer orphan cleanup guard:** `safeCleanupOrphans` requires the collection directory to exist before purging missing documents, protecting against unmounted or relocated sources wiping indexed collections.
- **Embeddings loopback & error bounding:** direct `VLClient` instances enforce loopback-only connections under `privacy.offline_only`; non-2xx error bodies are bounded to 2 KiB across all embedding/OCR/rerank/batch HTTP clients; batch polling terminates immediately on non-200 responses.
- **Hybrid search content fidelity:** RRF fusion preserves the full chunk payload and line-span attributes over 40-token BM25 snippets when merging vector and lexical hits for the same document.

## [0.5.8] - 2026-09-18

### Added

- **ZCode conversation support:** embedded `zcode` parser schema indexes
  `~/.zcode/cli/rollout` model-I/O JSONL (`seek add --parser zcode`), and the
  JSONL parser driver gained a sliding-window mode (`messages_path` +
  `offset_field`) that dedups cumulative per-request conversation windows by
  global index. A manually configured ZCode `Stop` hook in
  `~/.zcode/cli/config.json` keeps the index fresh (see `docs/service.md`).

## [0.5.7] - 2026-09-18

### Added

- Full local NLP setup for the semantic tag service: per-platform setup
  scripts (`tools/semantic/setup.sh|ps1`), model bootstrap, vendored-license
  policy, and a refreshed local semantic quickstart (`docs/semantic.md`).

### Fixed

Code chunking (correctness of indexed code content):

- Write block-packing overlap tails in forward line order — they were
  reversed, scrambling the head of every overlap-carrying code chunk.
- Fragment oversized code lines at UTF-8 rune boundaries instead of raw byte
  offsets, keeping non-ASCII content valid and matchable.
- Bound `AssignLineNumbers` lookahead and advance past unmatched chunks so
  spans no longer collapse onto the same lines.

Store integrity:

- Run the FTS tokenizer rebuild (DROP + CREATE + repopulate) in a real
  transaction and treat a missing `documents_fts` as rebuild-needed: a crash
  mid-migration can no longer leave keyword search silently empty.
- Run `DeleteCollection`'s five destructive DELETEs in one transaction; an
  interrupted `seek rm` leaves the collection intact.
- Propagate HNSW search errors instead of silently falling back to a linear
  scan; incremental vector sync distinguishes store errors from missing
  embeddings.
- Surface previously swallowed errors: surrounding-context and autocomplete
  row scans, fast-field summary iteration, zstd decompression during FTS
  rebuilds.
- Normalize HNSW `ef_search` so manifest round-trips do not trigger spurious
  rebuilds, and drop an unsynchronized global distance-func registration that
  raced concurrent flushes.
- Speed up indexing by running fast_fields DDL once per store instead of on
  every write; guard the pool with bounded connections and reject `?` in the
  database path before it corrupts the DSN.

Secrets and configuration:

- Mask API keys in `seek config` and `seek doctor --verbose` output.
- `seek auth login` persists only the fields it changes: `${VAR}` env
  indirection survives, plaintext expanded secrets and stale OCR/Rerank
  fallback keys are no longer baked into config.yaml.
- Abort startup on unreadable config files and failed config/cache directory
  creation; fail fast when the home directory cannot be resolved.

CLI behavior:

- Connect SIGINT/SIGTERM to long-running commands (sync, embed, reindex, rm,
  search) so Ctrl+C unwinds through cleanup instead of dying mid-write.
- Route progress and error diagnostics to stderr; result lines stay on stdout.
- Keep the underlying store error in rename/rm diagnostics instead of
  misreporting every failure as "collection not found"; surface systemd
  start/stop failures in `seek service`.
- `seek auth login` returns a non-zero exit code when aborted (Ctrl-C/EOF).

Search quality:

- Render unary `NOT` under `AND` as FTS5 binary NOT: `go AND NOT rust` no
  longer silently searches for just `go`.
- Give mixed-type fast-field sorting a total order; `--sort-by` on columns
  mixing strings and numbers no longer produces arbitrary orderings.
- Reject unrecognized punctuation in parsed queries (including inside NEAR)
  instead of silently dropping it; parsed mode falls back to raw as before.

Sources and parsers:

- Fail fast on collection patterns containing path separators (they could
  never match a basename) instead of silently indexing nothing.
- Percent-encode the path in parserdef SQLite `file:` URIs so `?`/`#`/`%` in
  a source path cannot drop `mode=ro` read-only enforcement.
- Re-assert the allowlist before interpolating a documents column into SQL.

## [0.5.6] - 2026-09-14

### Fixed

- Prevent HNSW duplicate-node panics during forced re-embedding by deferring
  live vector updates until the replacement graph is complete.
- Build full vector-index replacements off to the side and publish them
  atomically, preserving the previous usable graph when rebuilding fails.
- Restore the previous SQLite embedding when an existing-vector update cannot
  be reflected in HNSW, keeping persisted embeddings and vector search aligned.

## [0.5.5] - 2026-09-14

### Fixed

- Rebuild the vector index with the new embedding dimension before re-embedding
  during an explicitly allowed vector-space migration.
- Preserve and restore the searchable index state when migration or reindexing
  fails, including vector metadata, FTS entries, fast fields, and legacy NULL
  document metadata.
- Keep degraded embedding behavior intact for already-matching profiles and
  intentionally skipped image chunks.

## [Unreleased]

### Added

- `seek add` gains the canonical `--type markdown|code|documents|pdf|images` and
  `--agent claude|codex|opencode|copilot|zed|hermes` selectors (plan C2). Native
  Claude/Codex stay native; `copilot` maps to the `copilot-cli` parser schema.
  All existing flags (`--claude`, `--pdf`, `--opencode`, `--claude-schema`, …)
  keep working as aliases of the new syntax.

### Changed

- **`seek add` now rejects conflicting type selectors instead of silently
  resolving them.** Previously, combining two collection-type flags (e.g.
  `--code --pdf`) resolved to the first match in an internal if-chain with no
  indication that the other flag was ignored. Such combinations now fail fast
  with an explicit error:
  - two different native kinds (`--code --pdf`, `--claude --codex`,
    `--type code --pdf`, `--agent claude --type code`) →
    `conflicting collection types selected (…)`;
  - a native kind together with a parser selector
    (`--claude --parser foo`, `--code --opencode`) → same error;
  - two parser selectors (`--opencode --copilot`, `--claude-schema --codex-schema`)
    → `multiple parser sources selected (…)`.
  Aliases of the **same** kind are not conflicts and are still accepted
  (`--documents` ≡ `--docs` ≡ `--type documents`; `--code` ≡ `--type code`;
  `--claude-schema` ≡ `--parser claude`).
