# AGENTS.md — seek

Guidance for AI coding agents working in this repository.

## What this is

`seek` is a personal hybrid search engine (BM25 full-text + vector semantic search) for markdown notes, Claude Code conversations, and Codex conversations. It stores data in SQLite (via `mattn/go-sqlite3` with FTS5) and generates embeddings through a configurable provider (default DashScope).

Go 1.24 module `github.com/anthropics/seek`. ~5,200 LOC across a root package plus `cmd/` and `internal/`.

## Build / test / verify (always run these)

```bash
# Build the real binary (FTS5 tag is REQUIRED):
CGO_ENABLED=1 go build -tags fts5 -o /dev/null .     # or: make build

# Test — requires the fts5 tag (mattn/go-sqlite3 with FTS5). `make test` already
# includes it; the store tests open a real SQLite DB that needs FTS5.
go test -tags fts5 ./...        # or: make test

# Sanity:
go vet ./...        # clean
gofmt -l cmd internal main.go   # must print nothing (fix with gofmt -w)
```

- `mattn/go-sqlite3` is **cgo** — `CGO_ENABLED=1` is required to build.
- FTS5 is enabled by the `fts5` build tag. Without it, `store.Open` errors out at migrate time. This is why **every** build/test invocation needs `-tags fts5`.
- The `Makefile` `test` target runs `go test -tags fts5 ./...` (it was fixed to include the tag).

## Architecture (layered, no cycles)

```
root (main.go) ──> cmd ──> internal/{chunk,config,embed,search,source,store}
```

Keep the layering: `cmd/` orchestrates; `internal/` has no imports from `cmd/` or `root`. Do not introduce cross-boundary imports.

- `internal/store` — SQLite persistence: collections, documents, chunks, embeddings, FTS5 index, vector search. **All SQL lives here.** `SearchVector` currently does a full-table scan + cosine (SIMD via `viterin/vek`); it has no ANN index yet.
- `internal/embed` — providers: `Client` (any OpenAI-compatible text embeddings) and `VLClient` (multimodal image+text). The VL endpoint is configurable via `embedding.vl_base_url`, defaulting to DashScope's qwen3-vl-embedding.
- `internal/search` — BM25 + vector + RRF hybrid fusion. The RRF fusion and sort are covered by tests.
- `internal/source` — per-format parsers (claude/codex/markdown/images) plus `pdf.go`, which rasterizes PDF pages to PNG via `go-fitz` (bundled static MuPDF, cgo).
- `internal/chunk` — text chunking (header/size splitting).
- `internal/config` — `~/.config/seek/config.yaml`, provider selection, `IsMultimodal()` logic.

## Embedding provider configuration

Config lives at `~/.config/seek/config.yaml`:

```yaml
embedding:
  base_url: https://api.openai.com/v1   # any OpenAI-compatible endpoint
  api_key: ${OPENAI_API_KEY}
  model: text-embedding-3-small
  dimensions: 1024
  # multimodal: true                    # force image+text (VL) embedding
  # vl_base_url: https://...            # VL endpoint; unset → DashScope default
```

- Text embeddings go to `base_url + /v1/embeddings` (OpenAI-compatible).
- Multimodal (image+text) uses `VLClient`. It is enabled when `IsMultimodal()` is true — i.e. `multimodal: true`, or the model name contains `vl-embedding`/`multimodal`.
- `dimensions` is **fixed at index time** (stored per-chunk). Changing model or dimensions requires re-indexing (`seek rm <collection>` then `seek add`).
- **Never hardcode a provider endpoint** — everything must route through `base_url`/`vl_base_url` from config. The only exception is `DefaultVLEndpoint` as the DashScope fallback.

## Testing conventions

- Test files exist for `internal/chunk`, `internal/search`, `internal/source`, `internal/store`, `internal/config`, and `internal/embed`. There are **no tests** for `internal/embed/client.go` or `internal/embed/batch.go` yet.
- `internal/store` tests open a **real temp SQLite DB** via `t.TempDir()` and require the `fts5` tag.
- For new store tests, follow `internal/store/store_test.go` (uses `newTestStore(t)` helper + `t.Cleanup`).
- Benchmarks live in `internal/store/store_test.go` (`BenchmarkCosineSimilarity`, `BenchmarkBubbleSort`) — run with `go test -tags fts5 -bench . ./internal/store/`.

## Performance notes (things already optimized / to keep in mind)

- Cosine similarity uses SIMD via `github.com/viterin/vek/vek32`. The hand-rolled `cosineSimilarity` wrapper keeps guards against empty/mismatched/zero vectors (vek panics on degenerate input), so preserve those.
- Do not reintroduce O(n²) sorts — use `sort.Slice` (the old bubble sorts were replaced).
- Vector search is still a linear scan over all embedded chunks + in-memory sort. A proper ANN index (`sqlite-vec` `vec0` tables) is a known, scoped follow-up; note the `dimensions` is configurable, so a `vec0` table needs a fixed dimension decided at DDL time.

## Gotchas

- `os.UserHomeDir()` / `os.Stat` errors are often intentionally ignored (`home, _ := ...`) in `internal/source` — benign, but the `filepath.Walk` callback swallows traversal errors silently; don't copy that pattern for new critical paths.
- Keep `main.go` and all files `gofmt`-clean. Field-alignment drift is a recurring cosmetic issue in `main.go` and `internal/embed/vl_client.go`.
- SQLite is accessed through `database/sql`; connection strings use `_journal_mode=WAL&_foreign_keys=on`.

## Commands surface (for reference)

```
seek add <path> --name <n>   # add markdown/notes collection
seek add --claude | --codex | --images <path> | --pdf <path>
seek sync                    # incremental sync
seek embed                   # generate embeddings (realtime or batch)
seek search "<query>" [--lex] [--vec] [-l N]
seek status                  # collections + counts
seek auth login|status       # configure/show embedding provider
seek service|hooks           # periodic sync+embed service management
```
