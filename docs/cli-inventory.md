# Seek CLI Inventory & Command Classification

This document is the **canonical, committed record of the seek command surface**.
It is the single source of truth that two other mechanisms pin:

- **Golden `--help` snapshots** — `cmd`/root golden tests render the real kong
  grammar (`main.go`) and compare against `testdata/help/*.txt`. Regenerate
  with `go test -tags "fts5 sqlite_fts5" -update .`.
- **Stale-command CI check** — `scripts/check-stale-commands.sh` fails when
  `README.md`, `docs/` (excluding `plans/`), or `skills/` reference a `seek`
  command that is not in the canonical set below.

If you add, remove, or rename a top-level command, update this table **and**
regenerate the golden snapshots. Everything else (docs, skills, hooks, service)
is validated against this set.

## Canonical top-level command set

There are **19** top-level command entries: 18 leaf commands plus the
`advanced` group. This is verified by the `TestHelpGolden_TopLevel` snapshot,
which lists every command kong exposes. The `check-stale-commands.sh` check
derives its canonical token set from the snapshot's `Commands:` lines (awk
first field), so the `advanced schema` / `advanced parsers list` /
`advanced analyze` entries make `advanced` a canonical token there.

```
add  rm  collection  sync  embed  search  analyze  status  service
hooks  auth  config  advanced  schema  doctor  uninstall  parsers  fields  mcp
```

## Classification

Commands are grouped into four categories. The grouping is a **UX/visibility
concern only** — it never moves domain code or changes behavior (plan §3.2).

| Category        | Commands                                                        | Rationale |
|-----------------|-----------------------------------------------------------------|-----------|
| **core**        | `add`, `collection`, `sync`, `search`                          | The default add → sync → search axis; `collection` manages the index these operate on. |
| **maintenance** | `auth`, `config`, `embed`, `hooks`, `doctor`, `service`, `uninstall` | Provider credentials, config, periodic service, hooks, health/repair, teardown. |
| **advanced**    | `fields`, `mcp`, and the `advanced` group (`schema`, `parsers`, `analyze`) | Schema/agent/observability surfaces. `schema`/`parsers`/`analyze` live under `seek advanced` (C4); `fields`/`mcp` stay top-level advanced. |
| **compatibility** | `rm`, `status`                                                | Legacy aliases retained for existing user scripts/hooks. |

Notes and judgment calls:

- `rm` and `status` are **compatibility** aliases (plan §3.1, sibling plan).
  `status` is also named as a "primary" convenience view in plan §3.1, but it is
  a legacy alias, so it is classified compatibility here.
- `fields` is **advanced** (plan §3.2; shares its backbone with `search --field`
  and MCP `seek_fields`).
- `uninstall` and `config` are **maintenance** (plan §3.2).
- `doctor` and `mcp` appear in plan §3.1's "primary commands" block, but they are
  not on the add/sync/search/status core axis: `doctor` is health/repair
  (**maintenance**) and `mcp` is the agent tooling surface (**advanced**).
- `analyze`, `parsers`, `schema` now live under the `seek advanced` group (C4),
  with legacy top-level shims retained as compatibility aliases (plan §3.2, §5
  C4). The `advanced` group is a canonical top-level token; the shims keep
  `schema`/`parsers`/`analyze` canonical too.

## Command-usage inventory

Every place in the repo that invokes a seek command, grouped by surface. This is
an **inventory** (what exists today), not a refactor — the templates below are
left untouched until the later cards that change them.

### User-facing docs (validated by the stale-command CI check)

| File / glob            | Top-level commands referenced |
|------------------------|-------------------------------|
| `README.md`            | add, sync, search, collection, rm, status, embed, auth, mcp, fields, service, hooks, doctor, uninstall, advanced |
| `docs/semantic.md`     | sync, collection, search, fields |
| `docs/mcp.md`          | mcp, search, fields, status |
| `docs/query-guide.md`  | search, fields |
| `docs/service.md`      | service, sync, embed, hooks, status |
| `docs/parsers.md`      | advanced, add, search |
| `docs/models.md`       | add, rm, embed, search, status, doctor |
| `docs/extractors.md`   | add, sync |
| `docs/local-setup.md`  | add, sync, embed, search, doctor |
| `skills/seek/SKILL.md` | search, add, advanced, sync, embed, collection, status, rm, service, hooks, auth, config |
| `skills/seek/references/filters.md`    | search, fields |
| `skills/seek/references/collection-types.md` | add, collection, advanced, search, status |
| `skills/seek/references/troubleshooting.md`  | status, sync, embed, rm, add, doctor, advanced |
| `skills/seek/references/hooks.md`        | hooks, config |
| `skills/seek/references/service.md`      | service, sync, embed, status |
| `skills/seek/references/query-syntax.md` | search, advanced |
| `skills/seek/references/ocr.md`          | add, sync, search, doctor |

(`docs/plans/` is intentionally **excluded** from the CI check — the plan docs
describe future flag forms such as `seek advanced …` that do not exist yet.)

### Internal invocations (code that runs seek as a subprocess or builds its args)

| Location                        | Invocations (canonical commands) |
|---------------------------------|----------------------------------|
| `internal/agenthooks/command.go`| `sync --no-lock --type <agent>` (hook command strings), `hooks sync --agent <agent>`, `hooks context --agent <agent>` |
| `internal/agenthooks/*_test.go` | `seek sync`, `seek hooks sync`, `seek hooks context` (golden/matcher tests) |
| `cmd/service.go`                | `<bin> sync` (systemd/plist/schtasks templates) |
| `.github/workflows/release.yml` | `bin/seek status` (FTS5 smoke test) |
| `internal/search` (hook search) | `search <q> --lex -l 3 --doc-type <agent>` via `hookSearchArgs` |

The hook command strings (`sync --no-lock --type <agent>`, `hooks sync --agent`,
`search <q> --lex -l 3 --doc-type …`) are a **compatibility surface pinned by
golden tests** in `internal/agenthooks` — their exact format must not change
(plan AGENTS.md gotchas). The service template emits `<bin> sync`; the smoke
test runs `bin/seek status`.

## Config profiles

`seek config` groups the config file into three user-facing profiles. This is
**presentation only** — the config schema and file layout are unchanged (no
section is renamed, added, or dropped). The profile → section mapping is:

| Profile            | Config sections                                                        |
|--------------------|------------------------------------------------------------------------|
| `core`             | `search`, `filters`, `aggregations`, `privacy`, `chunk`, `db_path`, `cache_dir` |
| `local-semantic`   | `embedding` (incl. `vl_base_url` / `multimodal`), `rerank`, `semantic` |
| `document-extras`  | `ocr`, `extractor` (`backend` / `xberg_base_url`)                      |

Advanced-only knobs — `vector_index` (backend + HNSW tuning) and `compression` —
are shown **only** with `seek config --advanced` and are not written into a
freshly-created default config. The plain view prints a hint when any advanced
knobs are present. `seek doctor --verbose` prints the fully resolved config
(advanced knobs + every applied default) as part of the health check.

The plain `seek config` view is a read-only projection: it never rewrites or
drops advanced/unknown knobs from the on-disk config file (see the round-trip
test in `internal/config/profiles_test.go`).

## How to change the surface safely

1. Add/remove/rename a top-level command in `main.go` (kong struct).
2. Update this table (canonical set + classification + this inventory).
3. Regenerate golden snapshots: `go test -tags "fts5 sqlite_fts5" -update .`
4. Run the CI check locally: `sh scripts/check-stale-commands.sh`
5. Update any docs/skills that reference the changed command.
