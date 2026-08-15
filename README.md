# seek

Personal hybrid search engine for markdown notes, Claude Code conversations, and Codex conversations. BM25 full-text + vector semantic search with multimodal embedding.

## Why

AI agents lose context between sessions. `seek` indexes everything — your notes, every Claude Code conversation, every Codex session, including screenshots — so agents can recall what you discussed weeks ago.

## Install

**Linux / macOS** (requires Go 1.24+, CGO):
```bash
make build
ln -sf $(pwd)/seek /usr/local/bin/seek
```

**Windows** (PowerShell, requires Go 1.24+ and a C compiler like `zig` or MinGW `gcc`):
```powershell
$env:CC="zig cc"; $env:CGO_ENABLED="1"
go build -tags fts5 -o seek.exe .

# Optional: install directly to your Go bin (~/go/bin) or any directory in your PATH:
# go install -tags fts5 .
```

As a [skill](https://skills.sh) (Claude Code, Codex, Cursor, etc.):
```bash
# Copy the skill from this repo to your skills directory
mkdir -p ~/.agents/skills
cp -r skills/seek ~/.agents/skills/seek
```

## Quickstart

`seek` works in two modes. Pick the one that fits your needs — you can start keyword-only and add embeddings later without re-indexing.

### Option A — Keyword search only (no API key, fully offline)

`add` and `sync` never require an API key — they build the FTS5 index from your content's text. You only need a key for vector/semantic search. So if you just want fast keyword (BM25) search over your notes and conversations:

```bash
# No config, no API key — just start indexing
seek add ~/notes --name mynotes            # markdown
seek add ~/projects/myrepo --code          # source code (Go, Rust, Python, TS, etc.)
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
| `add --docs` (docx/xlsx/pptx/epub/html/...) | — | — | Rich formats via the xberg extraction backend |

### Migrating from Option A to B

You can start offline and add embeddings anytime — no re-indexing needed:

```bash
# 1. You've been using seek keyword-only with collections already indexed
seek auth login                            # add an embedding provider
seek embed                                 # backfills vectors for existing chunks
# Now `seek search "query"` uses hybrid (BM25 + vector) automatically
```

### Simplest 100% Local Setup (Ollama — Free, Private, Offline)

If you want semantic search without cloud API keys or internet access:

1. **Pull the model** (one-time):
   ```bash
   ollama pull nomic-embed-text
   ```
2. **Set your `~/.config/seek/config.yaml`**:
   ```yaml
   embedding:
     base_url: http://localhost:11434/v1
     api_key: ollama
     model: nomic-embed-text
     dimensions: 768
   ```
3. Run `seek embed` — vectors will be generated 100% locally on your machine.

---

### Choosing an Embedding Provider (Any OpenAI-Compatible Endpoint)

`seek` works with **any OpenAI-compatible embedding endpoint** (`POST {base_url}/embeddings`) — both cloud APIs and local/self-hosted servers:

```bash
seek auth login
```

Or configure directly in `~/.config/seek/config.yaml`:

```yaml
# --- Example 1: OpenAI (Cloud) ---
embedding:
  base_url: https://api.openai.com/v1
  api_key: ${OPENAI_API_KEY}
  model: text-embedding-3-small
  dimensions: 1536              # text-embedding-3-small supports 1536 (default) or 1024

# --- Example 2: Alibaba Cloud DashScope (Default Multimodal) ---
# embedding:
#   base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
#   api_key: ${DASHSCOPE_API_KEY}
#   model: text-embedding-v3
#   dimensions: 1024

# --- Example 3: Local Ollama (100% Offline & Private, Free) ---
# embedding:
#   base_url: http://localhost:11434/v1
#   api_key: ollama             # any non-empty string for local servers
#   model: nomic-embed-text     # run: ollama pull nomic-embed-text
#   dimensions: 768             # nomic-embed-text outputs 768 dimensions

# --- Example 4: Local vLLM / TEI / LM Studio / LocalAI ---
# embedding:
#   base_url: http://localhost:8000/v1
#   api_key: unused
#   model: BAAI/bge-m3
#   dimensions: 1024            # bge-m3 outputs 1024 dimensions
```

#### Embedding Models & Providers Comparison

| Provider / Model | Dimensions | Modality | Environment | Speed / Latency (CPU) | Code Search Support | Best For |
|---|---|---|---|---|---|---|
| **`nomic-embed-text`** (Ollama) | **768** | Text | Local (Ollama) | ~8–15 ms | **Good** (8K context window) | Simplest 1-click local & offline setup |
| **`BAAI/bge-m3`** (Local/TEI) | **1024** | Text / Multi | Local / Self-hosted | ~20–40 ms | **Exceptional** (100+ PLs & NLs) | SOTA local code, dense+sparse hybrid |
| **`text-embedding-3-small`** (OpenAI) | **1536** / 1024 | Text | Cloud API | ~60–100 ms | **High** | High accuracy general cloud embedding |
| **`qwen3-vl-embedding`** (DashScope) | **1024** | Multimodal (Text + Images) | Cloud API | ~100–180 ms | **High** | Visual search, PDFs & screenshot matching |
| **`text-embedding-v3`** (DashScope) | **1024** | Text | Cloud API | ~50–90 ms | **High** | Balanced default text embeddings |
| **`all-MiniLM-L6-v2`** (Local) | **384** | Text | Local CPU / Edge | ~2–5 ms | **Basic** | Ultra-lightweight & low-resource devices |

### Optional: Supercharging with Re-ranking (Local FlashRank / Cloud)

For deeper semantic precision (~15–25% boost over pure embeddings), you can optionally turn on Cross-Encoder re-ranking:

```yaml
# 100% Local & Fast: FlashRank / TEI with bge-reranker-small or MiniLM (CPU-friendly, ~5-15ms)
rerank:
  enabled: true
  base_url: http://localhost:8000/v1   # local FlashRank / TEI server
  api_key: local
  model: bge-reranker-small            # or ms-marco-MiniLM-L-12-v2
  top_n: 10
```

> See [Re-ranking Models Comparison](#re-ranking-models-comparison) for benchmarks across `bge-reranker-small`, `bge-reranker-v2-m3`, `Cohere`, and `Jina`.

> [!IMPORTANT]
> **Vector Dimensions Matching:**
> The `dimensions` setting in `config.yaml` **must exactly match** the output dimension of your chosen model (e.g. `1536` for OpenAI small, `768` for Nomic, `1024` for BGE-M3 / DashScope, `384` for MiniLM).
> `dimensions` is fixed per chunk at index time and defines the vector index layout. If you later switch to a model with a different dimension, run `seek rm <collection>`, `seek add`, and `seek embed` to rebuild the vector index.

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
seek add --docs /path/to/papers -n docs   # rich docs (docx/xlsx/pptx/epub/html/...) via xberg

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

# Extraction backend — how files become indexable text
extractor:
  backend: builtin          # "builtin" (markdown/pdf/images, default) or "xberg"
  output_format: markdown   # format requested from xberg: plain|markdown|djot|html
  xberg_base_url: http://127.0.0.1:8000   # xberg serve endpoint
  timeout: 180s             # per-request timeout for xberg extraction
```

### Document extraction (rich formats via xberg)

The builtin backend handles markdown, PDF, and images. For rich document formats
(docx/xlsx/pptx/epub/html/eml/csv/...), point seek at a running
[xberg](https://github.com/xberg-io/xberg) server:

```bash
# 1. Start xberg (one-time per session; see xberg docs for install)
xberg serve -p 8000 &

# 2. Configure seek to use it
#    either set extractor.backend: xberg in config.yaml (above)
#    or pass --backend xberg per command

# 3. Add a documents collection — formats auto-detected
seek add --docs ./reports/ --backend xberg      # docx, xlsx, pptx, epub, html, ...
seek sync                                        # re-extracts on subsequent runs
```

The `--backend` flag overrides the config default for a single command, so you
can mix backends: `--pdf` with the builtin (go-fitz rasterization) and `--docs`
with xberg in the same seek instance.

> **Note:** `dimensions` is fixed at index time. If you change models or dimensions, re-run `seek rm <collection>` and `seek add` to rebuild the index.

## Usage

```bash
# Hybrid search (BM25 + vector, recommended)
seek search "how to deploy the gateway"

# BM25 keyword search (fast, no API call)
seek search "ECONNREFUSED port 3000" --lex

# Vector semantic search (meaning-based)
seek search "functional programming architecture" --vec

# Precision Source Addressing & Context Expansion
seek search "handleRequest" --repo seek              # outputs file.go:L25-L68
seek search "handleRequest" -C 1                     # expands 1 chunk before & after for context

# Filtered search
seek search "error" --collection mynotes --doc-type markdown
seek search "conn" --repo myproject --lang go        # code collection filtering
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

**Background service** — periodic sync + embed with native OS scheduling:
- **Windows**: Windows Task Scheduler (`schtasks.exe`)
- **Linux**: `systemd` user timer (`systemctl --user`)
- **macOS**: `launchd` plist (`~/Library/LaunchAgents/`)

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
- `--doc-type <type>`: document type (markdown, claude, codex, images, pdf, documents, parser, code)
- `--lang <language>`: programming language for code collections (e.g. `go`, `python`, `typescript`)
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

**Ranking & Cross-Encoder Re-ranking** — BM25 results are weighted by column: title matches count 10× body matches (`bm25(documents_fts, 10.0, 1.0)`). When `rerank.enabled: true` is configured in `config.yaml`, the search engine automatically re-scores candidate hits using a cross-encoder model before returning the top results.

- **Bi-Encoder (Embedding)** generates vectors independently for speed.
- **Cross-Encoder (Re-ranker)** evaluates query + document pairs together with full attention layers, providing deep semantic relevance and a significant accuracy boost (~15–25% NDCG).
- **Local & Offline (FlashRank / TEI / LocalAI):** Use lightweight models like `bge-reranker-small` or `ms-marco-MiniLM-L-12-v2` (~25–90 MB, CPU-friendly, 5–15 ms latency, zero external API calls).
- **Cloud APIs:** Any endpoint supporting standard `POST /rerank` (DashScope, Cohere, Jina, SiliconFlow, or self-hosted TEI).

```yaml
# --- Example 1: Local Offline Re-ranking (FlashRank / TEI / LocalAI) ---
# Ultra-fast, runs on CPU with zero API keys or cloud dependencies:
rerank:
  enabled: true
  base_url: http://localhost:8000/v1
  api_key: local                       # any string for local servers
  model: bge-reranker-small            # or ms-marco-MiniLM-L-12-v2
  top_n: 10

# --- Example 2: Cloud / High-Precision Re-ranking (BGE-Large / Cohere / Jina / DashScope) ---
# rerank:
#   enabled: true
#   base_url: https://api.openai.com/v1   # or https://api.cohere.com/v1 / DashScope
#   api_key: ${RERANK_API_KEY}
#   model: bge-reranker-large            # or rerank-v3.5 / jina-reranker-v2
#   top_n: 10
```

#### Re-ranking Models Comparison

| Model | Parameters | Size (Disk / RAM) | Environment | Latency (CPU) | Code Search Support | Best For |
|---|---|---|---|---|---|---|
| **`bge-reranker-v2-m3`** | ~560M | ~560 MB (ONNX) / ~1.1 GB | Local GPU / Fast CPU | ~35–80 ms | **Exceptional** (100+ PLs & NLs) | SOTA multi-language & source code search |
| **`jina-reranker-v2-base`** | ~278M | ~300 MB / API | Local / Cloud API | ~30–70 ms | **High** (Code + 8K Context) | Large function bodies, repo search & API matching |
| **`bge-reranker-small`** | ~24M–45M | ~25 MB (ONNX) / ~90 MB | Local CPU (FlashRank/TEI) | ~5–15 ms | **Good** (General code + text) | Ultra-fast local desktop search, zero lag, private |
| **`bge-reranker-large`** | ~330M–560M | ~300 MB (Quant) / ~1.3 GB | Local GPU / Cloud | ~50–150 ms | **Very High** | High-precision multi-lingual & technical docs |
| **`ms-marco-MiniLM-L-12-v2`** | ~33M | ~30 MB (ONNX) / ~120 MB | Local CPU (FlashRank) | ~8–20 ms | **Basic** (Text prioritized) | Lightweight local keyword & English text ranking |
| **`cohere-rerank-v3.5`** | Cloud API | Remote API | Cloud Endpoint | ~100–250 ms | **High** (Code & Docs) | Zero local compute, large context windows |

**Precision Source Addressing & Context Expansion** — Search results output exact 1-based source line ranges (`path/to/file.go:L25-L68`), enabling immediate IDE and AI navigation. Using `-C N` / `--context N` automatically expands surrounding chunk windows before and after match hits for complete code context.

## Collections

| Type | Source | What's indexed |
|---|---|---|
| `markdown` | Any directory | `.md` files, FTS + chunks + embeddings |
| `code` | Any directory / repo | 35+ languages (Go, Rust, Python, TS, etc.), `.gitignore`-aware, structural chunks + fastfields |
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
