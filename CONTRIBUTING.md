# Contributing to seek

Thanks for your interest in contributing. `seek` is a Go project with a
cgo/SQLite dependency, so please read the build notes before starting.

## Prerequisites

- Go 1.25+ (`go.mod` pins `go 1.25.0`)
- CGO enabled (`CGO_ENABLED=1`) — `mattn/go-sqlite3` requires it
- FTS5 build tags: every build/test command needs `-tags "fts5 sqlite_fts5"`

## Build & test

```bash
CGO_ENABLED=1 go build -tags "fts5 sqlite_fts5" -o /dev/null .   # or: make build
CGO_ENABLED=1 go test  -tags "fts5 sqlite_fts5" ./...             # or: make test
CGO_ENABLED=1 go vet   -tags "fts5 sqlite_fts5" ./...
gofmt -l cmd internal main.go third_party   # must print nothing
```

On Windows, prefer `CC="zig cc"` / `CXX="zig c++"` for builds that link
`go-fitz` (see AGENTS.md).

## Development workflow

1. Fork and create a feature branch.
2. Make your change. Follow existing code patterns and conventions.
3. Add or update tests. Store tests open a real temp SQLite DB; follow
   `internal/store/store_test.go`.
4. Run the full quality gate above; all must pass.
5. Update `CHANGELOG.md` under `## [Unreleased]` for user-facing changes.
6. Open a pull request.

## Architecture constraints

- Layering: `cmd/` orchestrates; `internal/` never imports `cmd/` or root.
  Do not introduce cross-boundary imports.
- All SQL lives in `internal/store`.
- Source files are never modified by `seek` commands.

## Releasing

Releases are cut with `make release VERSION=X.Y.Z` (see AGENTS.md). Do not
cut releases manually unless you are a maintainer.

## Reporting issues

Use the issue templates under `.github/ISSUE_TEMPLATE/`. For security
issues, see [SECURITY.md](SECURITY.md) — do not open a public issue.