# seek

[![Build](https://img.shields.io/github/actions/workflow/status/ozgurulukir/seek/test.yml?branch=main)](https://github.com/ozgurulukir/seek/actions/workflows/test.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Version](https://img.shields.io/github/v/release/ozgurulukir/seek)](https://github.com/ozgurulukir/seek/releases)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](go.mod)

> **Personal hybrid search engine (BM25 + Vector + Re-ranking) for source code, markdown notes, and AI agent conversations — with a 100% local/offline mode.**

## About

`seek` gives humans and AI agents instant, unified recall across an entire
workspace. It indexes markdown notes, source code, PDFs, rich documents, and
AI agent conversations into a local SQLite database (FTS5) and combines
keyword (BM25) search with optional vector semantic search and cross-encoder
re-ranking. Everything stays on your machine: keyword search and all index data
are local, and a fully offline mode needs no API keys at all.

Who it's for: developers and knowledge workers who want fast, private search
over their notes, codebases, and agent chat history, plus AI coding agents
that need a single `seek search` instead of repeated grep/glob sweeps.

See [SECURITY.md](SECURITY.md) for the privacy/data-flow model and
[LICENSE](LICENSE) for terms.

## Quickstart

### Option A — Offline & Keyword Only (No API Key, No Network)

```bash
# Add collections
seek add ~/notes --name mynotes            # markdown
seek add ~/projects/myrepo --code          # source code (Go, Rust, Python, TS, etc.)
seek add --claude                          # Claude Code conversations
seek sync                                  # fast incremental index

# Instant BM25 search (with Turkish/Unicode diacritics normalization)
seek search "ECONNREFUSED port 3000" --lex
```

This option performs zero external network calls: indexing, keyword search, and
all data stay on your machine. Scanned-PDF OCR is available as an opt-in when
you point it at a local loopback vision server; remote OCR remains blocked by
`privacy.offline_only: true`.

### Option B — Hybrid Search (Cloud OpenAI / DashScope)

⚠️ **External data flow:** in this mode chunk text, queries, and (if enabled)
images are sent to the configured embedding/rerank/OCR endpoints. Nothing is
sent to seek's authors — there is no telemetry — but the providers you
configure do receive your data. `seek doctor` lists every active endpoint and
data type; `privacy.offline_only: true` in config.yaml refuses all of it.

```bash
seek auth login                            # configure base_url / api_key / model
seek embed                                 # generate vectors for indexed chunks
seek search "functional programming architecture"   # hybrid: BM25 + vector + RRF
```

### Option C — 100% Local Semantic Search (Ollama + FlashRank)

```bash
ollama pull nomic-embed-text               # local embedding
uv run tools/flashrank_server/server.py    # local cross-encoder reranker
seek auth login                            # choose option 3 (ollama)
seek embed --force                         # auto mode → realtime for local providers
seek search "how does vector search work" -C 1
```

Optional full local metadata enrichment (language, entities, keyphrases, and
topics) is provided by the semantic tag service. Install it once using the
platform-specific instructions in [the semantic service guide](docs/semantic.md),
then enable it in `config.yaml`:

```yaml
semantic:
  enabled: true
  base_url: http://127.0.0.1:8003
```

After starting the service, enrich existing collections with:

```bash
seek collection reindex <collection> --semantic-only
```

`uv run tools/semantic/server.py` is intentionally degraded mode (YAKE
keyphrases only). Full mode must be started with the Python interpreter under
`tools/semantic/.venv`; the guide includes Linux/macOS and Windows commands.

Ollama implements the OpenAI-compatible realtime `/v1/embeddings` endpoint,
but not the Files and Batch endpoints that `seek`'s async mode uses. `seek`
detects this automatically: with `embedding.mode: auto` (the default) a local
provider resolves to the realtime request batch, so no `--realtime` flag is
needed. Async batch is an optional throughput/cost optimization for hosted
providers that expose the Files + Batch API; it is never inferred from a
loopback address. Both paths produce the same stored vectors.

### Install

**Linux & macOS:**
```bash
curl -fsSL https://raw.githubusercontent.com/ozgurulukir/seek/main/install.sh | bash
```

The installer is fail-closed: the release checksum file is mandatory (missing
or mismatched checksums abort), archive contents are validated before
extraction (no symlinks or path traversal), and the installed binary is
smoke-tested — a broken install is rolled back to your previous binary
instead of being reported as success. To verify the download yourself before
running (download–hash–verify–run):

```bash
# Set VERSION to the latest release tag (https://github.com/ozgurulukir/seek/releases):
VERSION=vX.Y.Z
curl -fsSLO "https://github.com/ozgurulukir/seek/releases/download/${VERSION}/seek_${VERSION}_linux-amd64.tar.gz"
curl -fsSLO "https://github.com/ozgurulukir/seek/releases/download/${VERSION}/SHA256SUMS.txt"
grep "seek_${VERSION}_linux-amd64.tar.gz" SHA256SUMS.txt | sha256sum -c -
tar -xzf "seek_${VERSION}_linux-amd64.tar.gz" && ./seek --version
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/ozgurulukir/seek/main/install.ps1 | iex
```

<details>
<summary><b>Build from Source</b> (Requires Go 1.25+ and CGO)</summary>

**Linux / macOS:**
```bash
make build
ln -sf $(pwd)/bin/seek /usr/local/bin/seek
# or install directly: make install
```

**Windows (PowerShell):**
```powershell
$env:CC="zig cc"; $env:CGO_ENABLED="1"
go build -tags "fts5 sqlite_fts5" -o seek.exe .
```
</details>

**Install Agent Skill** (Claude Code, Codex, Antigravity, Cursor):
```bash
mkdir -p ~/.agents/skills/seek
cp -r skills/seek/* ~/.agents/skills/seek/
```

## Features

Core:
1. Hybrid fusion search — SQLite FTS5 (BM25) + HNSW vector search + RRF (Reciprocal Rank Fusion).
2. Source code & notes indexing — 35+ programming languages (`.gitignore`-aware), Markdown, PDFs, and rich documents.
3. AI agent memory — Claude Code, Codex, ZCode, Opencode, Copilot CLI, and Hermes Agent sessions, including multimodal screenshots.
4. Precision source addressing — 1-based line ranges (`file.go:L25-L68`) with surrounding context expansion (`-C 1`).
5. Honest privacy model — keyword search and all index data are local (index DB `0600`, dirs `0700`); cloud providers receive data only if configured; `seek doctor` shows exactly what goes where; `privacy.offline_only: true` blocks every external call.

Advanced:
6. Full local/offline mode — zero cloud keys required for keyword search; local Ollama (`nomic-embed-text`) + FlashRank (`ms-marco-TinyBERT`) cover semantic search and reranking on your machine.
7. Cross-encoder re-ranking — config-driven reranking via FlashRank, BGE-Reranker, Cohere, or Jina.
8. Multimodal embeddings — image+text VL embeddings (DashScope qwen3-vl-embedding) with configurable endpoint.
9. OCR for scanned PDFs — vision/OCR models extract text from scanned pages (local loopback only under offline mode).
10. Schema-driven parsers — opencode, Copilot CLI, Zed, Hermes, ZCode, and schema-mode Claude/Codex.
11. MCP server — expose the index to AI agents via Model Context Protocol (`seek mcp`).
12. Background service & hooks — periodic sync+embed via Task Scheduler / systemd / launchd; Claude Code & Codex hooks.
13. Semantic tag enrichment — optional local NLP service generating tags/topics/entities/language fast fields.
14. Compression — Zstd/LZ4 chunk content compression with transparent decompression.
15. Aggregations & autocomplete — faceted aggregations and prefix suggestions.

## Documentation

Deep-dive documentation and specialized guides (full index: [docs/index.md](docs/index.md)):

| Guide | Description |
|---|---|
| [100% Local & Offline Setup](docs/local-setup.md) | Ollama, FlashRank, semantic tagging & xberg setup, ASCII flow diagram, tool scripts. |
| [Models & Re-ranking Guide](docs/models.md) | Embedding & Cross-Encoder comparisons, dimension rules, CPU latency benchmarks. |
| [Background Service & Concurrency](docs/service.md) | Scheduled tasks (Windows Task Scheduler / systemd / launchd), hooks, SQLite WAL. |
| [Document Extractors & OCR](docs/extractors.md) | Builtin vs xberg extraction (100+ formats: docx, xlsx, pdf, html, csv) and OCR vision. |
| [Query Syntax & Filters Guide](docs/query-guide.md) | Structured AST syntax (AND/OR/NOT), filters (`--repo`, `--lang`), line spans (`:L10-L45`), and `-C`. |
| [Schema-Driven Parsers](docs/parsers.md) | Opencode, Copilot CLI, Zed threads, and `--workspace` filtering. |
| [AI Agent Skill Reference](skills/seek/SKILL.md) | Agent prompt instructions, query strategies, and CLI reference. |
| [Semantic Tag Service](docs/semantic.md) | Optional local NLP service (NER, keyphrases, topics, LID) generating `tags`/`topics`/`entities`/`language` fast fields. |
| [MCP Server](docs/mcp.md) | `seek mcp` — Model Context Protocol tools for Claude Code and other agents. |

## MCP Server

Expose your entire seek index to AI agents via the [Model Context Protocol](https://modelcontextprotocol.io):

```bash
seek mcp          # MCP server on stdio (JSON-RPC 2.0)
```

**Tools:** `seek_search` (hybrid search with the full filter/facet/sort surface), `seek_fields` (fast-field discovery and values), `seek_status` (collections + counts), `seek_capabilities` (search availability and lexical fallback), `seek_autocomplete` (prefix suggestions).

**Claude Code** — add to `~/.claude.json` (or project `.mcp.json`):
```json
{
  "mcpServers": {
    "seek": { "command": "seek", "args": ["mcp"] }
  }
}
```

Other agents (Codex CLI, Cursor, Zed, VS Code, Cline, etc.) use the same
stdio server with their own config path/section — see
[docs/mcp.md](docs/mcp.md) for ready-made snippets for each.

Reads only the local SQLite index; the `privacy.offline_only` setting applies unchanged.

## CLI Commands Cheat Sheet

```bash
# Indexing & Collections
seek add <path> --name <name>      # add markdown collection
seek add <path> --code             # add source code collection (35+ languages)
seek add <path> --documents        # add rich documents (docx/xlsx/pdf/html/csv via xberg)
seek add --claude                  # Claude Code sessions (+images)
seek add --codex                   # Codex sessions (+images)
seek add --opencode                # opencode sessions
seek add --copilot                 # GitHub Copilot CLI sessions
seek add --hermes                  # Hermes Agent sessions
seek add --parser zcode            # ZCode sessions (~/.zcode/cli/rollout)
seek sync                          # incremental index update then embed new chunks
seek sync --json                   # collection status/counts on stdout; progress on stderr
seek sync --strict                 # also fail on unavailable sources or embeddings
seek sync --no-embed               # index only (keyword-first: skip embedding entirely)
seek sync --realtime               # force the realtime request batch (used by stop-hooks)
seek sync <col> --path <p>         # validate <p> is inside <col>, then sync the whole collection
seek embed [--force|-f] [--realtime|-r] [--batch|-b]
                                    # generate embeddings; auto → realtime locally
                                    # --batch forces async Files + Batch API

# Collection lifecycle (index-only — source files are never modified)
seek collection list               # list collections with semantic coverage
seek collection show <name>        # detailed per-collection info
seek collection rename <old> <new> # rename the index label; source path unchanged
seek collection reindex <name>     # re-read source, rebuild chunks/FTS/fast-fields + re-embed
seek collection reindex <name> --semantic-only  # semantic fast-field backfill (no source/FTS/embeddings)
seek rm <name>                     # remove a collection from the index (compatibility alias)
seek status                        # view collections & counts (compatibility alias)

# Search & Navigation
seek search "<query>"              # hybrid search (BM25 + Vector + Re-ranking)
seek search "<query>" --json       # machine-readable JSON
seek search "<query>" --lex        # BM25 keyword search only
seek search "<query>" --vec        # Vector semantic search only
seek search "<query>" -C 1         # expand surrounding chunk context
seek search "<query>" --repo <r>   # filter by repository/collection
seek search "<query>" --lang <l>   # filter by code language
seek search "<query>" --field tags:<t>  # filter by fast field tags
seek search "<query>" --path <p>   # filter by file path pattern (GLOB)
seek search "<query>" --aggs "type:terms"  # faceted aggregations
seek fields [name] [--json]        # discover fast-field values & taxonomy
seek mcp                           # MCP server on stdio

# System & Management
seek auth login                    # configure embedding/rerank providers
seek auth status                   # inspect provider status
seek service start                 # start periodic sync+embed service
seek service stop                  # stop the service
seek service status                # inspect the service
seek hooks install                 # install automatic conversation sync hooks
seek hooks uninstall               # remove automatic conversation sync hooks
seek doctor [--fix-permissions]    # audit & repair private data permissions (0700/0600)
seek uninstall --dry-run           # remove service/hooks/cache/config (preview first!)
seek advanced analyze "<text>" --lang en  # tokenize and stem
seek advanced parsers list        # view parser schemas and detection status
```

> **Source files are never modified.** No `seek` management command — `add`,
> `sync` (incl. `sync <collection> --path`), `embed`, `collection
> rename`/`reindex`, the `--semantic-only` backfill, or `rm` — ever modifies,
> deletes, or renames your source files. Every command only reads your files
> and manages the local index; your files remain the source of truth.

During bulk sync, absent auto-discovered conversation sources are reported as
`skipped` warnings. An explicitly selected missing source and any indexing
failure return a nonzero exit status. `--strict` also fails on skipped sources
or unavailable embeddings; `--no-embed` remains an intentional skip. `--json`
emits an array of collection reports with `status`, counts, errors, and
embedding availability.

If no embedding provider is configured, `seek sync` still succeeds — it prints
a single "skip embeddings … run `seek auth login`" hint, leaves chunks pending,
and keyword (BM25) search keeps working fully. `seek embed` and `seek search
--vec` will tell you exactly what's missing; `privacy.offline_only: true`
disables embeddings outright. Use `seek sync --no-embed` for an intentional
keyword-first workflow.

## Configuration

Config lives at `~/.config/seek/config.yaml` (see [CONTRIBUTING.md](CONTRIBUTING.md)
for development). `seek config` prints it grouped into three profiles (schema
and file layout unchanged):

```yaml
embedding:
  base_url: http://127.0.0.1:11434/v1
  api_key: ollama
  model: nomic-embed-text
  dimensions: 768

rerank:
  enabled: true
  base_url: http://127.0.0.1:8000
  api_key: local
  model: ms-marco-TinyBERT-L-2-v2
  top_n: 10

extractor:
  backend: builtin          # "builtin" or "xberg"
  xberg_base_url: http://127.0.0.1:8001
  output_format: markdown

search:
  query_mode: parsed        # "parsed" (AST) or "raw" (FTS5 passthrough)
  default_limit: 10
  rrf_k: 60

vector_index:
  backend: hnsw             # "hnsw" (default) or "linear"
  hnsw:
    m: 16
    ef_search: 50
    persist_path: ~/.cache/seek/hnsw.index

compression:
  algorithm: zstd           # "zstd" (default), "lz4", or "none"
  level: 3

semantic:
  enabled: false            # optional local tag enrichment
  base_url: http://127.0.0.1:8003
  max_tags: 5

privacy:
  offline_only: true        # refuse external embedding/rerank/OCR/xberg calls
```

Profiles printed by `seek config`: **core** (`search`, `filters`, `aggregations`,
`privacy`, `chunk`, `db_path`, `cache_dir`); **local-semantic** (`embedding`
incl. `vl_base_url`/`multimodal`, `rerank`, `semantic`); **document-extras**
(`ocr`, `extractor`). Advanced-only knobs (`vector_index`, `compression`) are
shown only via `seek config --advanced`; `seek doctor --verbose` prints the
fully resolved config. `seek config` / `seek doctor` never rewrite your config
file.

## Built With & Attributions

- [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) — SQLite with FTS5
- [coder/hnsw](https://github.com/coder/hnsw) — HNSW vector indexing
- [viterin/vek](https://github.com/viterin/vek) — SIMD-accelerated vector math
- [FlashRank](https://github.com/Prithivida/FlashRank) & [BAAI](https://github.com/FlagOpen/FlagEmbedding) — Cross-Encoder re-ranking
- [klauspost/compress](https://github.com/klauspost/compress) — Zstd chunk compression
- See [ATTRIBUTION.md](ATTRIBUTION.md) for full upstream licenses and credits.

## License

MIT — see [LICENSE](LICENSE). This project is a fork of
[ethan-huo/seek](https://github.com/ethan-huo/seek), which licenses its code
under MIT; its copyright notice is retained in our LICENSE. Security policy and
reporting: [SECURITY.md](SECURITY.md). Contributing: [CONTRIBUTING.md](CONTRIBUTING.md).