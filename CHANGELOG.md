# Changelog

All notable changes to `seek` are documented here. This follows
[Keep a Changelog](https://keepachangelog.com/) conventions.

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
