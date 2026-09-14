# ADR-0001 — CLI drop decisions (post-compatibility release)

- **Status:** Proposed (decision record for a FUTURE compatibility release)
- **Date:** 2026-09-14
- **Related plan:** `docs/plans/2026-09-13-cli-surface-simplification-plan.md` §5 C7, §4, §8
- **Scope:** Documentation only. This ADR records decision rationale and a
  checklist. It deletes no code and changes no behavior.

## Context

The CLI surface was simplified (plan cards C1–C6): `seek advanced` was created
(schema/parsers/analyze moved under it, legacy top-level paths became compat
shims with stderr deprecation), `add` was normalized to `--type`/`--agent`/
`--parser` with conflict validation, search help was grouped (advanced flags
under an "Advanced" section), config output was split into core /
local-semantic / document-extras profiles with advanced-only knobs
(`vector_index`, `compression`), and `doctor --verbose` gained effective
defaults.

The compatibility policy (plan §4) is: old commands/flags keep working for at
least one minor release; removal decisions are made **manually** after a
compatibility release, based on repo-wide usage, issue/feedback, and
compatibility cost. This ADR is that decision record. **No removal happens
here** — each candidate below is removed (if at all) in a separate future ADR
and release, after the checklist in §Decision Checklist is satisfied.

The five questions below are the ones plan §5 C7 lists. Each is grounded in the
current codebase state (verified at HEAD `167fa9d` + working-tree C1–C6).

## Decision

### 1. `--docs` vs `--documents` alias — keep `--documents`, deprecate `--docs`

Both flags exist on `add` (`cmd/add.go`): `--documents` is the primary flag,
`--docs` is the shortcut alias. Both map to the same `CollectionTypeDocuments`
(`internal/app/add.go`), and both are equivalent to `--type documents`.

- **Pros of keeping `--documents`:** it is the descriptive, unambiguous name; it
  matches the canonical `--type documents` selector and the `documents`
  collection type; docs and skills already treat it as primary
  (`skills/seek/references/collection-types.md`, `skills/seek/SKILL.md`).
- **Pros of keeping `--docs`:** it is shorter to type; a few docs still use it
  (`README.md`, `docs/extractors.md`).
- **Cons of `--docs`:** it is a two-character abbreviation that reads as
  ambiguous next to `--documents`; keeping two spellings of the same selector
  doubles the surface to document and test.

**Recommendation:** keep `--documents` as the canonical flag; deprecate `--docs`
after the compatibility window. Update the few docs that still use `--docs`
(`README.md`, `docs/extractors.md`) to `--documents` before removal.

### 2. Top-level `schema`/`parsers`/`analyze` shims — remove after one minor release

C4 made the legacy top-level paths compat shims that delegate to the same Run
functions as `seek advanced schema|parsers|analyze`, emitting a stderr-only
deprecation warning (`cmd/schema.go`, `cmd/parsers.go`, `cmd/analyze.go`
`BeforeApply`; `cmd/advanced.go` `deprecate`).

- **Pros of removing:** these are introspection-only commands; the canonical
  `seek advanced` group is the documented surface; the deprecation warning
  already points users at the canonical path; keeping both paths doubles the
  help surface and the golden-test surface.
- **Cons of removing:** any user script or skill that still calls `seek schema`
  / `seek parsers` / `seek analyze` breaks (they are not in the core
  add/sync/search axis, so usage is expected to be low).

**Recommendation:** remove the three top-level shims after one minor release,
keeping only `seek advanced schema|parsers|analyze`. The `advanced` group stays
canonical. Verify with the checklist (repo-wide grep for `seek schema`,
`seek parsers`, `seek analyze` in docs/skills/hooks/service) before removal.

### 3. CLI autocomplete — keep the CLI flags; MCP `seek_autocomplete` is preserved

`--autocomplete`/`--autocomplete-max` were moved to the advanced help section
(`cmd/search.go`); the MCP `seek_autocomplete` tool is preserved
(`cmd/mcp.go`).

- **Pros of keeping CLI autocomplete:** it is a small, self-contained feature
  with its own JSON output path (`search.NewAutocompleteOutput`); it is useful
  for shell scripting and interactive use without an MCP client; the advanced
  grouping already reduces its default visibility.
- **Cons of keeping:** it is a second surface for the same capability as MCP
  `seek_autocomplete`; it is not on the core add/sync/search axis.
- **Pros of dropping CLI autocomplete:** one less flag pair to document/test.
- **Cons of dropping:** breaks any scripted autocomplete; the feature is already
  cheap to maintain and hidden from the default help.

**Recommendation:** keep the CLI `--autocomplete`/`--autocomplete-max` flags.
They are advanced-only visibility, low-cost, and independently useful. Do not
drop them; MCP `seek_autocomplete` remains the agent-facing surface.

### 4. `linear` backend + compression knobs — keep as supported, advanced-only

`vector_index.backend` (`hnsw`/`linear`) and `compression` are advanced-only
visibility in `seek config` (`internal/config/profiles.go`); `doctor --verbose`
shows them as effective defaults.

- **Pros of keeping as supported:** `linear` is a legitimate, explicitly
  configured fallback for small corpora where an HNSW index is unnecessary
  overhead; compression is a real storage optimization (Zstd/LZ4) with
  backward-compatible reads. Both are genuine product surfaces, not test
  scaffolding.
- **Cons of keeping as supported:** they are not on the core axis and add
  configuration surface; a user could misconfigure them.
- **Pros of marking debug-only / dropping:** smaller supported surface.
- **Cons of dropping:** `linear` is the only non-HNSW vector backend and is
  referenced by the `VectorIndex` interface contract (`Clear()`); compression is
  a documented storage feature. Removing either would be a real capability loss,
  not just a visibility change.

**Recommendation:** keep both as **supported but advanced-only** surfaces. Do
not mark them debug-only and do not drop them. Their advanced-only visibility
already keeps them out of the default config and default help.

### 5. `--claude-schema`/`--codex-schema` — keep as top-level advanced aliases

C2 mapped these to `--parser claude|codex` (schema-driven, text-only variant of
native Claude/Codex). The schema variant is an advanced override, not the
canonical default (native `--claude`/`--codex` remain canonical).

- **Pros of keeping top-level visibility:** they are unambiguous, self-describing
  flags; they map cleanly to `--parser claude|codex`; docs/skills already use
  them (`skills/seek/references/collection-types.md`, `skills/seek/SKILL.md`).
- **Cons of keeping:** they are a second spelling of `--parser claude|codex`;
  the schema variant is not canonical, so top-level visibility slightly
  overstates their importance.
- **Pros of moving under advanced:** aligns visibility with the "advanced
  override" status.
- **Cons of moving:** breaks any script that uses them; they are already
  unambiguous and low-cost.

**Recommendation:** keep them as top-level compatibility aliases during the
compatibility window, documented as advanced overrides of `--parser claude|codex`.
After the window, either keep them (they are cheap and unambiguous) or move them
under `seek advanced add` if the surface is deemed too large. This is the
lowest-priority removal of the five.

## Decision checklist

Before removing any candidate, the maintainer must satisfy **all** of the
following (plan §4: removal is manual, based on repo-wide usage, issue/feedback,
and compatibility cost):

1. **Repo-wide usage grep** — search the whole repo (docs, skills, hooks,
   service templates, `.github/workflows`, `install.*`, `scripts/`) for the
   command/flag being removed. The canonical inventory is
   `docs/cli-inventory.md`; the stale-command CI check
   (`scripts/check-stale-commands.sh`) must still pass after the change.
2. **Issue/feedback** — check open issues and user feedback for reliance on the
   removed surface. No telemetry exists, so this is the only usage signal.
3. **Compatibility cost** — estimate how many user scripts/hooks would break and
   whether the deprecation warning has been live for at least one minor release.
4. **Docs/skills/hooks/service references** — update every reference to the
   removed surface in `README.md`, `docs/*.md`, `skills/seek/**`, hook command
   strings (`internal/agenthooks/command.go`), and service templates
   (`cmd/service.go`) to the canonical form in the same release.
5. **Golden/parity test impact** — regenerate golden `--help` snapshots
   (`go test -tags "fts5 sqlite_fts5" -update .`) and update any legacy-alias
   parity tests that assert the removed surface. Confirm the JSON wire format
   (`internal/search/wire.go`) is untouched.
6. **Separate ADR** — record the removal in a new ADR and a dedicated release;
   do not bundle it with unrelated changes.

## Status table

| # | Candidate | Current state (after this plan) | Proposed action | Trigger for removal |
|---|-----------|--------------------------------|-----------------|---------------------|
| 1 | `--docs` alias | `add` compat alias of `--documents` (both → `--type documents`) | Deprecate `--docs`; keep `--documents` canonical | After one minor release; docs updated to `--documents` |
| 2 | Top-level `schema`/`parsers`/`analyze` shims | Compat shims delegating to `seek advanced *` with stderr deprecation | Remove shims; keep `seek advanced` group | After one minor release; grep shows no repo usage |
| 3 | CLI `--autocomplete`/`--autocomplete-max` | Advanced help section; MCP `seek_autocomplete` preserved | Keep (advanced-only) | None — do not remove |
| 4 | `linear` backend + compression knobs | Advanced-only config visibility; `doctor --verbose` effective defaults | Keep as supported, advanced-only | None — do not remove |
| 5 | `--claude-schema`/`--codex-schema` | Top-level aliases of `--parser claude\|codex` (advanced override) | Keep as top-level advanced aliases; optionally move under advanced later | Lowest priority; only if surface deemed too large |

## Consequences

- **Positive:** the supported product surface shrinks to the add/sync/search
  axis plus the `seek advanced` group; the five decisions give the maintainer a
  concrete, evidence-based removal plan for the next compatibility release.
- **Negative:** none immediate — this ADR changes no behavior. Future removals
  (items 1, 2, and possibly 5) will break any user still relying on the legacy
  surface, which is why each is gated on the checklist and a separate release.
- **Neutral:** items 3 and 4 are explicitly retained, so their surfaces remain
  supported (advanced-only visibility) rather than being dropped.

## Out of scope

- Deleting any parser type, Store schema, or implementation because it is hidden
  from the CLI (plan §4: "Parser türleri veya Store şeması sırf CLI'dan
  gizlendiği için silinmez").
- Adding write authority to MCP.
- Redesigning the Store schema for CLI aesthetics (plan §8).
