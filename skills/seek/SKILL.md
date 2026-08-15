---
name: seek
description: Search user's personal notes, source code repositories (Go, Rust, Python, TS, etc.), Claude Code and Codex conversation history. Use when user asks about past conversations, code implementations, notes, or "do I have notes or code about X".
license: MIT
compatibility: Requires seek binary installed and in PATH. Hybrid/vector search requires an embedding API key configured in ~/.config/seek/config.yaml.
metadata:
  author: ozgurulukir
  version: "1.1"
allowed-tools: Bash(seek:*) Read
---

# seek — Personal Knowledge & Source Code Search

`seek` is the user's local search engine. It indexes source code repositories, markdown notes, Claude Code conversations, Codex conversations, rich documents, and CLI agent sessions with BM25 + vector hybrid search. Text and images share the same vector space via a configurable embedding provider — defaults to **qwen3-vl-embedding** (multimodal) on Alibaba Bailian (DashScope), but any OpenAI-compatible provider works.

Binary location: `seek`

## When to Use

- User asks "have I discussed X before" / "find my notes about Y" / "where is function Z implemented"
- User asks to search code across repositories or within a specific repository/language
- User references past conversations, notes, or codebases
- You need context from previous work sessions or source code

## Quick Start

```bash
# Global search across the entire index (all repositories, notes, and sessions)
seek search "your query" -l 10

# Search inside a specific repository or collection
seek search "func Open" --repo myproject -l 10

# Search by programming language
seek search "handleRequest" --lang typescript -l 10

# Search with surrounding context window expansion
seek search "handleRequest" --repo myproject -C 1 -l 5

# BM25 keyword search only (fast, offline, no API call)
seek search "exact keyword" --lex -l 10

# Vector semantic search only (meaning-based)
seek search "conceptual question" --vec -l 10
```

## Search Strategy

1. **Unfiltered Search:** Running `seek search "query"` searches across **the entire index** (all collections, repositories, notes, and agent sessions).
2. **Start with `--lex`** for exact terms, code symbols, names, error messages, file paths.
3. **Use default (hybrid)** for conceptual questions like "how to deploy" or "authentication middleware implementation".
4. **Use `--vec`** only when hybrid results are poor and you need pure semantic matching.
5. **Use filters** to narrow results:
   - `--repo <name>` / `--collection <name>`: target a specific repository or collection
   - `--lang <language>`: target a programming language (e.g. `go`, `python`, `typescript`, `rust`)
   - `--doc-type <type>`: `code`, `markdown`, `claude`, `codex`, `pdf`, `documents`, `parser`
   - `--after/--before`, `--chunk-type`, `--path`, `--workspace`
6. **Increase `-l 20`** if the first 10 results aren't enough.
7. **Use `--aggs`** to get facet counts and statistics alongside search results.

## Reading Results

```
# Text result with precise source line range
[1] internal/embed/rerank.go
    ~/internal/embed/rerank.go:L1-L41  (seek)  score=0.9500
    Snippet of matching content...

# Image result
[2] Conversation Title
    ~/.claude/projects/.../xxx.jsonl  (claude-conversations)  score=0.6037
    ~/.cache/seek/images/claude-0222d48a-0.png
    context: the dialog layout is broken, content overflows...
```

- Results include exact line spans like `path/to/file.go:L10-L45` for instant IDE/agent jumping and referencing.
- Pass `-C 1` or `-C 2` to expand surrounding chunk context before and after matching lines.
- `collection-name` tells you which collection the result came from (run `seek status` to see all)
- For conversation results, the title is the first user message
- The path is the actual file — use `Read` tool to get full content if needed
- Image results show the file path (`.png`/`.jpg`) — use `Read` tool to view it

## Maintenance Commands

Only run these when user explicitly asks to update the index:

```bash
# Sync new/changed files (incremental, fast)
seek sync

# Generate embeddings for new chunks
seek embed

# Force re-embed all chunks (e.g. after model change)
seek embed -f

# Check index status
seek status

# Add a new markdown collection
seek add /path/to/dir --name myname

# Add a source code repository collection
seek add /path/to/repo --code --name myrepo

# Add rich documents collection (docx/xlsx/pdf/...)
seek add /path/to/docs --docs

# Add agent conversation collections
seek add --claude        # Claude Code conversations
seek add --codex         # Codex conversations
seek add --opencode      # opencode CLI sessions
seek add --copilot       # GitHub Copilot CLI sessions
seek add --parser <name> # any parser schema by name

# List available parser schemas + detection status
seek parsers list
```

## Detailed References

- [Query Syntax](references/query-syntax.md) — boolean, phrase, prefix, fuzzy, field-scoped, proximity
- [Filters](references/filters.md) — repo/collection, lang, doc-type, date range, chunk-type, path, workspace
- [Collection Types](references/collection-types.md) — code, markdown, claude, codex, images, pdf, parser
- [Troubleshooting](references/troubleshooting.md) — no results, API errors, index issues

## Important Notes

- **Do NOT run `sync` or `embed` proactively.** Only when user asks to update.
- **Always use absolute path** to the binary: `seek`
- Hybrid/vec search requires API key (already configured in `~/.config/seek/config.yaml`)
- If search returns no results, try rephrasing or switching between `--lex` and hybrid
- Multilingual queries work — the index supports mixed-language content
- Works with any OpenAI-compatible endpoint (OpenAI, DashScope, Ollama, vLLM, etc.); `dimensions` in `config.yaml` must match the model output dimension (e.g. 1536 for OpenAI small, 768 for Nomic, 1024 for BGE-M3)
- Vector search uses HNSW index (persisted at `~/.cache/seek/hnsw.index`) for fast approximate nearest neighbor search; falls back to linear scan if the index is missing or corrupt
- Chunk content is compressed with Zstd by default to reduce storage; uncompressed content remains readable
- Query parsing is enabled by default; invalid syntax falls back to raw FTS5 MATCH automatically
