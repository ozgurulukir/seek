# seek

> **Personal hybrid search engine (BM25 + Vector + Re-ranking) for source code, markdown notes, and AI agent conversations with 100% local/offline support.**

`seek` gives humans and AI agents instant, unified recall across your entire workspace:
- 🔍 **Hybrid Fusion Search:** SQLite FTS5 (BM25) + HNSW Vector Search + RRF (Reciprocal Rank Fusion)
- 💻 **Source Code & Notes:** 35+ programming languages (`.gitignore`-aware), Markdown, PDFs, and rich documents
- 🤖 **AI Agent Memory:** Claude Code, Codex, Opencode, and Copilot CLI sessions (including multimodal screenshots)
- 🎯 **Precision Source Addressing:** Precise 1-based line ranges (`file.go:L25-L68`) with surrounding context expansion (`-C 1`)
- ⚡ **100% Local / Offline Support:** Zero cloud keys required; optionally supercharged with local Ollama (`nomic-embed-text`) and FlashRank (`ms-marco-TinyBERT`)
- 🧠 **Cross-Encoder Re-ranking:** Config-driven reranking via FlashRank, BGE-Reranker, Cohere, or Jina

---

## 📦 Install

**Linux / macOS** (requires Go 1.24+, CGO):
```bash
make build
ln -sf $(pwd)/seek /usr/local/bin/seek
```

**Windows** (PowerShell, requires Go 1.24+ and a C compiler like `zig` or MinGW `gcc`):
```powershell
$env:CC="zig cc"; $env:CGO_ENABLED="1"
go build -tags fts5 -o seek.exe .

# Optional: install directly to your PATH (~/go/bin):
# go install -tags fts5 .
```

### Install Agent Skill

Install the `seek` skill directly into your AI coding agent (Claude Code, Codex, Antigravity, Cursor):

```bash
mkdir -p ~/.agents/skills/seek
cp -r skills/seek/* ~/.agents/skills/seek/
```

---

## ⚡ Quickstart

### Option A — 100% Offline & Keyword Only (No API Key)
```bash
# Add collections
seek add ~/notes --name mynotes            # markdown
seek add ~/projects/myrepo --code          # source code (Go, Rust, Python, TS, etc.)
seek add --claude                          # Claude Code conversations
seek sync                                  # fast incremental index

# Instant BM25 search (with Turkish/Unicode diacritics normalization)
seek search "ECONNREFUSED port 3000" --lex
```

### Option B — Hybrid Search (Cloud OpenAI / DashScope)
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
seek embed -f -r                           # local realtime embedding
seek search "how does vector search work" -C 1
```

---

## 📚 Documentation Wiki

Deep-dive documentation and specialized guides:

| Guide | Description |
|---|---|
| 📖 [**100% Local & Offline Setup**](docs/local-setup.md) | Ollama, FlashRank & xberg setup, ASCII flow diagram, tool scripts. |
| 📊 [**Models & Re-ranking Guide**](docs/models.md) | Embedding & Cross-Encoder comparisons, dimension rules, CPU latency benchmarks. |
| ⚙️ [**Background Service & Concurrency**](docs/service.md) | Scheduled tasks (Windows Task Scheduler / systemd / launchd), hooks, SQLite WAL. |
| 📑 [**Document Extractors & OCR**](docs/extractors.md) | Builtin vs xberg extraction (100+ formats: docx, xlsx, pdf, html, csv) and OCR vision. |
| 🔍 [**Query Syntax & Filters Guide**](docs/query-guide.md) | Structured AST syntax (AND/OR/NOT), filters (`--repo`, `--lang`), line spans (`:L10-L45`), and `-C`. |
| 🤖 [**Schema-Driven Parsers**](docs/parsers.md) | Opencode, Copilot CLI, Zed threads, and `--workspace` filtering. |
| 🛠️ [**AI Agent Skill Reference**](skills/seek/SKILL.md) | Agent prompt instructions, query strategies, and CLI reference. |

---

## 🕹️ CLI Commands Cheat Sheet

```bash
# Indexing & Collections
seek add <path> --name <name>      # add markdown collection
seek add <path> --code             # add source code collection (35+ languages)
seek add <path> --docs             # add rich documents (docx/xlsx/pdf/html/csv via xberg)
seek add --claude | --codex        # add agent conversation sessions (+images)
seek add --opencode | --copilot    # add schema-driven agent sessions
seek sync                          # incremental index update
seek embed [-f] [-r]               # generate embeddings (batch or realtime)

# Search & Navigation
seek search "<query>"              # hybrid search (BM25 + Vector + Re-ranking)
seek search "<query>" --lex        # BM25 keyword search only
seek search "<query>" --vec        # Vector semantic search only
seek search "<query>" -C 1         # expand surrounding chunk context
seek search "<query>" --repo <r>   # filter by repository/collection
seek search "<query>" --lang <l>   # filter by code language (go, rust, python, ts, etc.)
seek search "<query>" --path <p>   # filter by file path pattern (GLOB)
seek search "<query>" --aggs "type:terms"  # faceted aggregations

# System & Management
seek status                        # view collections, document & chunk counts
seek auth login | status           # configure / inspect embedding & rerank providers
seek service start | stop | status # manage periodic OS background sync service
seek hooks install | uninstall     # install automatic conversation sync hooks
seek analyze "<text>" --lang en|tr # tokenize and stem text
seek parsers list                  # view parser schemas and detection status
```

---

## ⚙️ Configuration (`~/.config/seek/config.yaml`)

```yaml
embedding:
  base_url: http://localhost:11434/v1
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
  default_limit: 20
  rrf_k: 60

vector_index:
  backend: hnsw             # "hnsw" (default) or "linear"
  hnsw:
    m: 16
    ef_search: 50
    persist_path: ~/.cache/seek/hnsw.index
    dimension: 768          # must match embedding.dimensions

compression:
  algorithm: zstd           # "zstd" (default) or "none"
  level: 3
```

---

## 🏛️ Built With & Attributions

- [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) — SQLite with FTS5
- [coder/hnsw](https://github.com/coder/hnsw) — HNSW vector indexing
- [viterin/vek](https://github.com/viterin/vek) — SIMD-accelerated vector math
- [FlashRank](https://github.com/Prithivida/FlashRank) & [BAAI](https://github.com/FlagOpen/FlagEmbedding) — Cross-Encoder re-ranking
- [klauspost/compress](https://github.com/klauspost/compress) — Zstd chunk compression
- See [ATTRIBUTION.md](ATTRIBUTION.md) for full upstream licenses and credits.

## 📄 License

MIT
