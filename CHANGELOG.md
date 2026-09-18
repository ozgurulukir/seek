# Changelog

All notable changes to `seek` are documented here. This follows
[Keep a Changelog](https://keepachangelog.com/) conventions.

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
