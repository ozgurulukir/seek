---
name: seek
description: Search user's personal notes, markdown docs, Claude Code and Codex conversation history. Use when user asks about past conversations, notes, or "do I have notes about X".
license: MIT
compatibility: Requires seek binary installed and in PATH. Hybrid/vector search requires an embedding API key configured in ~/.config/seek/config.yaml.
metadata:
  author: ozgurulukir
  version: "1.0"
allowed-tools: Bash(seek:*) Read
---

# seek — Personal Knowledge Search

`seek` is the user's local search engine. It indexes markdown notes, Claude Code conversations, Codex conversations, and opencode sessions with BM25 + vector hybrid search. Text and images share the same vector space via a configurable embedding provider — defaults to **qwen3-vl-embedding** (multimodal) on Alibaba Bailian (DashScope), but any OpenAI-compatible provider works.

Binary location: `seek`

## When to Use

- User asks "have I discussed X before" / "find my notes about Y"
- User references past conversations or notes
- You need context from previous work sessions
- User asks you to search their knowledge base

## Quick Start

```bash
# Hybrid search (BM25 + vector, best quality, RECOMMENDED)
seek search "your query" -l 10

# BM25 keyword search only (fast, no API call)
seek search "exact keyword" --lex -l 10

# Vector semantic search only (meaning-based)
seek search "conceptual question" --vec -l 10
```

## Search Strategy

1. **Start with `--lex`** for exact terms, names, error messages, file paths
2. **Use default (hybrid)** for conceptual questions like "how to deploy" or "best practices for X"
3. **Use `--vec`** only when hybrid results are poor and you need pure semantic matching
4. **Increase `-l 20`** if the first 10 results aren't enough
5. **Use filters** to narrow results: `--collection`, `--doc-type`, `--after/--before`, `--chunk-type`, `--path`, `--workspace`
6. **Use `--aggs`** to get facet counts and statistics alongside search results

## Reading Results

```
# Text result
[1] Document Title
    ~/path/to/file.md  (collection-name)  score=0.5775
    Snippet of matching content...

# Image result
[2] Conversation Title
    ~/.claude/projects/.../xxx.jsonl  (claude-conversations)  score=0.6037
    ~/.cache/seek/images/claude-0222d48a-0.png
    context: the dialog layout is broken, content overflows...
```

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
- [Filters](references/filters.md) — collection, doc-type, date range, chunk-type, path, workspace
- [Collection Types](references/collection-types.md) — markdown, claude, codex, images, pdf, parser
- [Troubleshooting](references/troubleshooting.md) — no results, API errors, index issues

## Important Notes

- **Do NOT run `sync` or `embed` proactively.** Only when user asks to update.
- **Always use absolute path** to the binary: `seek`
- Hybrid/vec search requires API key (already configured in `~/.config/seek/config.yaml`)
- If search returns no results, try rephrasing or switching between `--lex` and hybrid
- Multilingual queries work — the index supports mixed-language content
- Vector search uses HNSW index (persisted at `~/.cache/seek/hnsw.index`) for fast approximate nearest neighbor search; falls back to linear scan if the index is missing or corrupt
- Chunk content is compressed with Zstd by default to reduce storage; uncompressed content remains readable
- Query parsing is enabled by default; invalid syntax falls back to raw FTS5 MATCH automatically
