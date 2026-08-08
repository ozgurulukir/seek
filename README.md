# seek

Personal hybrid search engine for markdown notes, Claude Code conversations, and Codex conversations. BM25 full-text + vector semantic search with multimodal embedding.

## Why

AI agents lose context between sessions. `seek` indexes everything — your notes, every Claude Code conversation, every Codex session, including screenshots — so agents can recall what you discussed weeks ago.

## Install

```bash
# requires: go 1.24+, CGO
make build
ln -sf $(pwd)/seek /usr/local/bin/seek
```

As a [skill](https://skills.sh) (Claude Code, Codex, Cursor, etc.):

```bash
bunx skills add ethan-huo/seek
```

## Quickstart

`seek` works in two modes. Pick the one that fits your needs — you can start keyword-only and add embeddings later without re-indexing.

### Option A — Keyword search only (no API key, fully offline)

`add` and `sync` never require an API key — they build the FTS5 index from your content's text. You only need a key for vector/semantic search. So if you just want fast keyword (BM25) search over your notes and conversations:

```bash
# No config, no API key — just start indexing
seek add ~/notes --name mynotes            # markdown
seek add --claude                          # Claude Code conversations
seek add --codex                           # Codex sessions
seek add --opencode                        # opencode CLI sessions
seek sync                                  # incremental update

# Search with BM25 (keyword) ranking — title matches boosted 10x
seek search "ECONNREFUSED port 3000" --lex
seek search "deploy gateway" --lex -l 5
```

This runs entirely offline. The FTS5 tokenizer is Turkish/diacritics-aware (`unicode61 remove_diacritics 2`), so `İstanbul`, `istanbul`, and `ISTANBUL` all match the same way.

### Option B — Hybrid search (keyword + semantic, recommended)

For meaning-based search ("functional programming architecture" matches docs about FP even without those exact words), add an embedding provider. Any [OpenAI-compatible](https://platform.openai.com/docs/api-reference/embeddings) endpoint works.

```bash
seek auth login                            # configure base_url / api_key / model
seek embed                                 # generate vectors for indexed chunks
seek search "functional programming architecture"   # hybrid: BM25 + vector + RRF
```

### What needs what

| Command / feature | API key | OCR | Notes |
|---|---|---|---|
| `add` / `sync` (text collections) | — | — | Always works offline |
| `search --lex` (BM25) | — | — | Keyword search, offline |
| `search` (hybrid, without key) | — | — | **Auto-falls back to BM25** when vector search unavailable |
| `search` (hybrid, with key) | required | — | BM25 + vector + RRF fusion |
| `search --vec` (semantic) | required | — | Vector-only |
| `embed` | required | — | Generates embeddings |
| `add --pdf` (text-based PDFs) | — | — | Embedded text indexed via FTS5 |
| `add --pdf` (scanned PDFs, searchable) | — | required | OCR extracts text from rasterized pages |

### Migrating from Option A to B

You can start offline and add embeddings anytime — no re-indexing needed:

```bash
# 1. You've been using seek keyword-only with collections already indexed
seek auth login                            # add an embedding provider
seek embed                                 # backfills vectors for existing chunks
# Now `seek search "query"` uses hybrid (BM25 + vector) automatically
```

### Choosing an embedding provider

```bash
seek auth login
# DashScope (default, qwen3-embeddings, free tier available):
#   base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
# OpenAI:
#   base_url: https://api.openai.com/v1
# Local (Ollama, vLLM, etc.):
#   base_url: http://localhost:11434/v1
```

> **Note:** `dimensions` is fixed at index time. If you later change models or dimensions, run `seek rm <collection>` then `seek add` to rebuild, and `seek embed` again.

## Setup

```bash
# Configure embedding API (DashScope / OpenAI / any OpenAI-compatible / custom)
seek auth login

# Add your collections
seek add /path/to/notes --name mynotes    # markdown
seek add --claude                          # Claude Code conversations (native, +images)
seek add --codex                           # Codex conversations (native, +images)
seek add --images /path/to/images -n pics  # image files (png/jpg/webp)
seek add --pdf /path/to/pdfs -n docs      # PDFs (pages rasterized for VL embedding)

# Schema-driven parser collections (text-only, no image extraction)
seek add --opencode                        # opencode CLI sessions (SQLite)
seek add --copilot                         # GitHub Copilot CLI sessions
seek add --zed                             # Zed Agent panel threads (zstd blobs)
seek add --parser <name>                   # any parser schema by name
seek add --claude-schema                   # Claude conversations via schema engine
seek add --codex-schema                    # Codex conversations via schema engine

# List available parser schemas + detection status
seek parsers list

# Generate embeddings
seek embed
```

Configuration lives in `~/.config/seek/config.yaml` (created by `seek auth login`):

```yaml
embedding:
  base_url: https://api.openai.com/v1          # any OpenAI-compatible endpoint
  api_key: ${OPENAI_API_KEY}                    # or a literal key
  model: text-embedding-3-small
  dimensions: 1024
  # multimodal: true                            # enable image+text embedding
  # vl_base_url: https://your-provider/v1/...   # defaults to DashScope if unset

ocr:
  # extract text from scanned PDF pages so they're keyword-searchable too
  enabled: true
  # base_url/api_key/model default to the embedding provider; model defaults to qwen-vl-ocr
  # base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
  # api_key: ${DASHSCOPE_API_KEY}
  # model: qwen-vl-ocr

# Search configuration
search:
  query_mode: parsed        # "raw" (FTS5 passthrough) or "parsed" (structured queries)
  default_limit: 20
  rrf_k: 60

# Vector index backend
vector_index:
  backend: hnsw             # "hnsw" (default) or "linear"
  hnsw:
    m: 16                   # HNSW M parameter
    # ef_construction: 100  # reserved; coder/hnsw v0.6.1 does not expose this parameter
    ef_search: 50           # query-time search width (higher = more accurate, slower)
    persist_path: ~/.cache/seek/hnsw.index
    dimension: 1024         # must match embedding.dimensions

# Filter configuration
filters:
  enabled: true
  default_collection: ""    # empty = all collections

# Aggregation configuration
aggregations:
  enabled: true

# Compression configuration
compression:
  algorithm: zstd           # "zstd" (default), "lz4", or "none"
  level: 3                  # compression level (1-22 for zstd)
```

> **Note:** `dimensions` is fixed at index time. If you change models or dimensions, re-run `seek rm <collection>` and `seek add` to rebuild the index.

## Usage

```bash
# Hybrid search (BM25 + vector, recommended)
seek search "how to deploy the gateway"

# BM25 keyword search (fast, no API call)
seek search "ECONNREFUSED port 3000" --lex

# Vector semantic search (meaning-based)
seek search "functional programming architecture" --vec

# Filtered search
seek search "error" --collection mynotes --doc-type markdown
seek search "meeting" --after 2024-01-01 --before 2024-12-31
seek search "image" --chunk-type image
seek search "doc" --path "docs/*.md"
seek search "deploy" --workspace /path/to/project   # parser collections only

# Aggregations
seek search "error" --aggs "type:terms"
seek search "error" --aggs "created_at:histogram:month"

# Analyze text (tokenize, stem)
seek analyze "running" --lang en
seek analyze "kitaplar evlerde" --lang tr

# Show schema
seek schema --show
seek schema --validate

# Autocomplete suggestions
seek search "hel" --autocomplete

# Incremental sync + embed new content
seek sync && seek embed
```

## Automation

**Background service** — periodic sync + embed via launchd (macOS):

```bash
seek service start              # every 1 hour (default)
seek service start -i 1800      # every 30 minutes
seek service stop
seek service status
```

**AI tool hooks** — auto-sync after every conversation:

```bash
seek hooks install              # adds Stop hook to Claude Code
seek hooks uninstall
```

This writes a `Stop` hook into `~/.claude/settings.json` so `seek sync` runs automatically when Claude finishes a conversation. Combined with the background service (which handles `embed`), your index stays current without manual intervention.

## How It Works

**Indexing** — `seek sync` scans collections incrementally. Markdown files are tracked by content hash. Claude/Codex JSONL files are append-only, tracked by line count. Base64 images in conversations are extracted to `~/.cache/seek/images/`. PDF pages are rasterized to PNG under `~/.cache/seek/pdf/` so VL models can embed them (DashScope's multimodal API accepts `image/*` data URIs, not PDFs directly). Embedded PDF text is indexed for keyword search; scanned pages are OCR'd when `ocr.enabled` is set, so their text is keyword-searchable too.

**Embedding** — `seek embed` generates vectors. Any [OpenAI-compatible](https://platform.openai.com/docs/api-reference/embeddings) provider works out of the box (set `base_url`/`model` in config, or run `seek auth login`). Multimodal image+text embedding uses a vision-language model and is enabled automatically for models matching `vl-embedding`/`multimodal`, or explicitly via `multimodal: true` — the VL endpoint defaults to DashScope's [qwen3-vl-embedding](https://help.aliyun.com/zh/model-studio/developer-reference/multimodal-embedding) but can be pointed at any provider via `vl_base_url`.

**Search** — Three modes:
- `--lex`: SQLite FTS5 BM25 ranking
- `--vec`: Cosine similarity against stored embeddings (HNSW index for fast search)
- Default (hybrid): [RRF fusion](https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf) combining both

**Query syntax** — Structured queries are supported by default:
- Boolean: `term1 AND term2`, `term1 OR term2`, `NOT term`
- Phrase: `"exact phrase"`
- Prefix: `pref*`
- Fuzzy: `term~2` (maps to prefix expansion)
- Field-scoped: `title:term`, `content:term`
- Proximity: `NEAR(term1 term2, 5)`

**Filters** — Narrow results by:
- `--collection <name>`: collection name
- `--doc-type <type>`: document type (markdown, claude, codex, images, pdf, parser)
- `--after <date>` / `--before <date>`: date range (RFC3339)
- `--chunk-type <type>`: chunk type (text, image)
- `--path <pattern>`: path pattern (GLOB syntax)
- `--workspace <path>`: workspace directory (parser collections only)

**Aggregations** — Get facet counts and statistics:
- `--aggs "type:terms"`: term aggregation by document type
- `--aggs "collection:terms"`: term aggregation by collection
- `--aggs "created_at:histogram:month"`: time-based histogram
- `--aggs "line_count:range:0-100,100-500"`: numeric range buckets
- `--aggs "count"`: total match count

**Storage** — SQLite database at `~/.cache/seek/index.db`. HNSW index at `~/.cache/seek/hnsw.index`. Config at `~/.config/seek/config.yaml`. Chunk content is compressed with Zstd (configurable) to reduce storage footprint; uncompressed content is still readable for backward compatibility.

**Query parsing** — By default, queries are parsed into structured AST (boolean, phrase, prefix, fuzzy, field-scoped, proximity). Invalid syntax falls back to raw FTS5 MATCH automatically. Use `--query-mode raw` to disable parsing.

**Tokenization** — Query-time analysis supports English and Turkish stemming (Snowball) with stop-word removal. Stemmed terms are expanded with `*` prefix for FTS5 matching, so "running" → "run*" matches both "run" and "running". Index-time tokenization uses FTS5's `unicode61` with `remove_diacritics 2`, which enables full Unicode case-folding — Turkish `İ/ı`, `ç/ğ/ş/ü/ö` and other Latin diacritics are normalized consistently at index and query time. Changing the tokenizer triggers a one-time automatic rebuild of the FTS index on the next `seek` invocation.

**Ranking** — BM25 results are weighted by column: title matches count 10× body matches (`bm25(documents_fts, 10.0, 1.0)`), so documents whose *title* matches the query rank above body-only matches.

## Collections

| Type | Source | What's indexed |
|---|---|---|
| `markdown` | Any directory | `.md` files, FTS + chunks + embeddings |
| `claude` | `~/.claude/projects/` | All Claude Code conversations + screenshots |
| `codex` | `~/.codex/` | All Codex sessions + screenshots |
| `images` | Any directory | Image files (png/jpg/webp) with VL embedding |
| `pdf` | Any directory | PDF pages rasterized to PNG, VL embedding per page + OCR text (if enabled) |
| `parser` | External SQLite/JSONL | Schema-driven: opencode, copilot-cli, zed, claude (text-only), codex (text-only) |

### Schema-driven parsers

New conversation platforms can be indexed without writing Go code — they're defined by declarative YAML schemas. The engine has a fixed Go motor; per-platform knowledge lives in schema files. Built-in schemas are embedded in the binary and can be overridden by user files.

**Built-in schemas** (run `seek parsers list` to see detection status):

| Schema | Driver | Source |
|--------|--------|--------|
| `opencode` | sqlite | `~/.local/share/opencode/opencode*.db` |
| `copilot-cli` | sqlite | `~/.copilot/session-store.db` |
| `zed` | sqlite | `~/.local/share/zed/threads/threads.db` (zstd blobs) |
| `claude` | jsonl | `~/.claude/projects/**/*.jsonl` (text-only, no images) |
| `codex` | jsonl | `~/.codex/sessions/**/*.jsonl` (text-only, no images) |

**User overrides** — drop a YAML file in `~/.config/seek/parsers/<name>.yaml` to completely replace a built-in schema (no merge). Useful for customizing queries or adapting to upstream format changes without waiting for a seek update.

**`--workspace` filter** — parser collections extract workspace metadata (`cwd`, `directory`, `folder_paths`) into a common `workspace` field, enabling cross-platform filtering:

```bash
seek search "deploy" --workspace /home/user/myproject
```

This works across opencode, copilot-cli, zed, and schema-driven claude collections (native claude/codex do not extract workspace metadata).

## Built With

- [Kong](https://github.com/alecthomas/kong) — struct-tag CLI framework
- [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) — SQLite with FTS5
- [coder/hnsw](https://github.com/coder/hnsw) — HNSW vector index for fast semantic search
- [blevesearch/snowballstem](https://github.com/blevesearch/snowballstem) — English + Turkish stemming
- [blevesearch/vellum](https://github.com/blevesearch/vellum) — FST-based autocomplete
- [klauspost/compress](https://github.com/klauspost/compress) — Zstd compression for chunk content
- [qwen3-vl-embedding](https://help.aliyun.com/zh/model-studio/developer-reference/multimodal-embedding) — multimodal embedding via DashScope

## License

MIT
