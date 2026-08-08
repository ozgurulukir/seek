# AGENTS.md — seek

Guidance for AI coding agents working in this repository.

## What this is

`seek` is a personal hybrid search engine (BM25 full-text + vector semantic search) for markdown notes, Claude Code conversations, and Codex conversations. It stores data in SQLite (via `mattn/go-sqlite3` with FTS5) and generates embeddings through a configurable provider (default DashScope).

Go 1.24 module `github.com/ozgurulukir/seek`. ~9,300 LOC across a root package plus `cmd/` and `internal/`.

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
root (main.go) ──> cmd ──> internal/{chunk,config,embed,indexer,search,source,store}
```

Keep the layering: `cmd/` orchestrates; `internal/` has no imports from `cmd/` or `root`. Do not introduce cross-boundary imports.

- `internal/store` — SQLite persistence: collections, documents, chunks, embeddings, FTS5 index, vector search. **All SQL lives here** (incl. `fastfield.go`, `vector_index.go`, `compression.go`). Vector search uses an HNSW index (`VectorIndex` interface, `coder/hnsw`) with a linear-scan fallback; cosine uses SIMD via `viterin/vek`. FTS5 tokenizer is `unicode61 remove_diacritics 2` (Turkish-aware); BM25 weights title 10× content. Migrate-time logic auto-rebuilds the FTS table when the tokenizer config changes.
- `internal/indexer` — orchestrates per-format sync: scans sources, upserts documents/chunks/FTS, writes fast-field metadata, runs orphan cleanup. This is the layer that knows about collection types (markdown/claude/codex/images/pdf/parser); `store` and `source` stay format-agnostic.
- `internal/embed` — providers: `Client` (any OpenAI-compatible text embeddings), `VLClient` (multimodal image+text), and `OCRClient` (OpenAI-compatible vision/OCR for scanned PDF pages). The VL endpoint is configurable via `embedding.vl_base_url`, defaulting to DashScope's qwen3-vl-embedding.
- `internal/search` — BM25 + vector + RRF hybrid fusion, plus a structured query parser (`query.go`), aggregations (`aggregation.go`), autocomplete (`autocomplete.go`), and a stemming analyzer (`tokenizer.go`). The RRF fusion and sort are covered by tests.
- `internal/source` — per-format parsers (claude/codex/markdown/images) plus `pdf.go`, which rasterizes PDF pages to PNG via `go-fitz` (bundled static MuPDF, cgo) and extracts text (embedded via `doc.Text`, or OCR via the `TextExtractor` interface for scanned pages). `source/parserdef/` holds the schema-driven parser engine (YAML schemas → SQLite/JSONL drivers) for opencode/copilot-cli/zed and schema-mode claude/codex.
- `internal/chunk` — text chunking (header/size splitting for markdown, line-batching for conversations).
- `internal/config` — `~/.config/seek/config.yaml`, provider selection, `IsMultimodal()` logic, and typed defaults (`defaults.go`).

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

### OCR config (`ocr`)

```yaml
ocr:
  enabled: true
  # base_url/api_key/model default to the embedding provider; model defaults to qwen-vl-ocr
  # base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
  # api_key: ${DASHSCOPE_API_KEY}
  # model: qwen-vl-ocr
```

- OCR runs only for PDF pages with no embedded text (scanned docs), during `seek add --pdf` / `seek sync`.
- Uses the OpenAI-compatible chat-completions vision format (`POST {base_url}/chat/completions`), so any vision/OCR model works.
- Extracted text is written to the chunk `content` and indexed via `UpsertFTS`, making scanned PDFs keyword-searchable.
- `source.TextExtractor` is the interface; `embed.OCRClient` implements it. Keep `source` decoupled from `embed`.

## Testing conventions

- Test files exist for `internal/{chunk,config,embed,indexer,search,source,store}` and `internal/source/parserdef`. There are **no tests** for `internal/embed/client.go` or `internal/embed/batch.go` yet.
- `internal/store` tests open a **real temp SQLite DB** via `t.TempDir()` and require the `fts5` tag.
- For new store tests, follow `internal/store/store_test.go` (uses `newTestStore(t)` helper + `t.Cleanup`).
- Benchmarks live in `internal/store/store_test.go` — run with `go test -tags fts5 -bench . ./internal/store/`.

## Performance notes (things already optimized / to keep in mind)

- Cosine similarity uses SIMD via `github.com/viterin/vek/vek32`. The hand-rolled `cosineSimilarity` wrapper keeps guards against empty/mismatched/zero vectors (vek panics on degenerate input), so preserve those.
- Do not reintroduce O(n²) sorts — use `sort.Slice` (the old bubble sorts were replaced).
- Vector search uses an HNSW index (`coder/hnsw`) when `vector_index.backend = hnsw` (default), falling back to a linear scan + in-memory sort otherwise. The index persists to `vector_index.hnsw.persist_path` (default `~/.cache/seek/hnsw.index`); `dimension` is fixed from `embedding.dimensions` at index build time.
- `Store.SyncVectorIndex` **clears the index before re-adding all embedded chunks** — this is intentional, so repeated `seek embed` calls do not accumulate duplicate/stale entries. `Clear()` is part of the `VectorIndex` interface. When adding a new index backend, implement `Clear()`.
- Chunk content can be Zstd-compressed (`compression.algorithm`); `GetChunkContent`/`SearchVector` transparently decompress. Compression is backward-compatible (uncompressed content still readable).

## Gotchas

- `os.UserHomeDir()` / `os.Stat` errors are often intentionally ignored (`home, _ := ...`) in `internal/source` — benign, but the `filepath.Walk` callback swallows traversal errors silently; don't copy that pattern for new critical paths.
- Keep `main.go` and all files `gofmt`-clean. Field-alignment drift is a recurring cosmetic issue in `main.go` and `internal/embed/vl_client.go`.
- SQLite is accessed through `database/sql`; connection strings use `_journal_mode=WAL&_foreign_keys=on`.
- **FTS5 tokenizer is fixed at CREATE time** — the constant `store.FTSTokenize` (`unicode61 remove_diacritics 2`) cannot be a bound parameter; it is formatted into the DDL. `migrate()` detects a tokenize change via `sqlite_master` and atomically drops + recreates + repopulates `documents_fts` from chunk contents (lossy reconstruction — see `rebuildFTSFromDocuments` comment). Don't change `FTSTokenize` casually; it triggers a full FTS rebuild for every user on next open.
- **FTS5 bm25 weighting** — `store.FTSTitleWeight` (10.0) boosts the title column over content. If you add columns to `documents_fts`, update the weight list in `SearchFTS` (SELECT and ORDER BY) — column weights are positional.
- **`expandEnv` uses `os.ExpandEnv`** — it substitutes `$VAR` and `${VAR}` anywhere in the value (e.g. `"Bearer ${TOKEN}"`), not just whole-value matches. Set undefined vars are left empty.
- The indexer logs `WARN:` lines and counts `failed` files per sync; previously these errors were silently dropped. New sync code paths should follow this pattern (count failures, surface them in the summary line).

## Commands surface (for reference)

```
seek add <path> --name <n>   # add markdown/notes collection
seek add --claude | --codex | --images <path> | --pdf <path>
seek add --opencode | --copilot | --zed | --parser <name>   # schema-driven parser collections
seek sync                    # incremental sync
seek embed                   # generate embeddings (realtime or batch)
seek search "<query>" [--lex] [--vec] [-l N] [--collection ...] [--aggs ...]
seek analyze "<text>"        # tokenize + stem
seek status                  # collections + counts
seek schema --show|--validate
seek parsers list            # list parser schemas + detection status
seek config                  # show/edit config
seek auth login|status       # configure/show embedding provider
seek service|hooks           # periodic sync+embed service management
```
