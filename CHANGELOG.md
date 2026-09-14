# Changelog

All notable changes to `seek` are documented here. This follows
[Keep a Changelog](https://keepachangelog.com/) conventions.

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
