---
name: seek
description: Search user's personal notes, markdown docs, Claude Code and Codex conversation history. Use when user asks about past conversations, notes, or "do I have notes about X".
---

# seek — Personal Knowledge Search

`seek` is the user's local search engine. It indexes markdown notes, Claude Code conversations, and Codex conversations with BM25 + vector hybrid search. Text and images share the same vector space via a configurable embedding provider — defaults to **qwen3-vl-embedding** (multimodal) on Alibaba Bailian (DashScope), but any OpenAI-compatible provider works.

Binary location: `seek`

## When to Use

- User asks "have I discussed X before" / "find my notes about Y"
- User references past conversations or notes
- You need context from previous work sessions
- User asks you to search their knowledge base

## Search Commands

```bash
# Hybrid search (BM25 + vector, best quality, RECOMMENDED)
seek search "your query" -l 10

# BM25 keyword search only (fast, no API call)
seek search "exact keyword" --lex -l 10

# Vector semantic search only (meaning-based)
seek search "conceptual question" --vec -l 10
```

### Query Syntax

Structured queries are parsed by default:

```bash
# Boolean
seek search "term1 AND term2"
seek search "term1 OR term2"
seek search "NOT term"

# Phrase
seek search '"exact phrase"'

# Prefix
seek search "pref*"

# Fuzzy (maps to prefix expansion)
seek search "term~2"

# Field-scoped
seek search "title:term"
seek search "content:term"

# Proximity
seek search "NEAR(term1 term2, 5)"
```

Invalid syntax falls back to raw FTS5 MATCH automatically.

### Filters

```bash
seek search "query" --collection mynotes
seek search "query" --type markdown
seek search "query" --after 2024-01-01 --before 2024-12-31
seek search "query" --chunk-type image
seek search "query" --path "docs/*.md"
```

Filters work with both `--lex` and `--vec` modes.

### Aggregations

```bash
seek search "query" --aggs "type:terms"
seek search "query" --aggs "collection:terms"
seek search "query" --aggs "created_at:histogram:month"
seek search "query" --aggs "line_count:range:0-100,100-500"
seek search "query" --aggs "count"
```

### Autocomplete

```bash
seek search "hel" --autocomplete
```

### Text Analysis

```bash
seek analyze "running" --lang en    # English stemming: [run]
seek analyze "kitaplar" --lang tr   # Turkish stemming: [kitap]
```

### Schema

```bash
seek schema --show      # Show field types and options
seek schema --validate  # Validate schema against DB
```

### Search Strategy

1. **Start with `--lex`** for exact terms, names, error messages, file paths
2. **Use default (hybrid)** for conceptual questions like "how to deploy" or "best practices for X"
3. **Use `--vec`** only when hybrid results are poor and you need pure semantic matching
4. **Increase `-l 20`** if the first 10 results aren't enough
5. **Use filters** to narrow results: `--collection`, `--type`, `--after/--before`, `--chunk-type`, `--path`
6. **Use `--aggs`** to get facet counts and statistics alongside search results

### Reading Output

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
# Also extracts base64 images from Claude/Codex conversations → ~/.cache/seek/images/
seek sync

# Generate embeddings for new chunks (VL multimodal API, text + images)
seek embed

# Force re-embed all chunks (e.g. after model change)
seek embed -f

# Check index status
seek status

# Add a new markdown collection
seek add /path/to/dir --name myname

# Add an image directory (png/jpg/webp — VL embedding for visual search)
seek add --images /path/to/images -n myimages

# Add a PDF directory (each page rasterized to PNG, then VL-embedded)
seek add --pdf /path/to/pdfs -n mydocs
```

## Collection Types

| Type | Source | What's indexed |
|---|---|---|
| `markdown` | Any directory | `.md` files — FTS + chunks + embeddings |
| `claude` | `~/.claude/projects/` | Claude Code conversations + screenshots |
| `codex` | `~/.codex/` | Codex sessions + screenshots |
| `images` | Any directory | Image files (png/jpg/webp) with VL embedding |
| `pdf` | Any directory | PDF pages rasterized to PNG, VL embedding per page + OCR text (if enabled) |

Run `seek status` to see which collections the user has configured.

## Multimodal

- Images from conversations are extracted and cached at `~/.cache/seek/images/`
- PDF pages are rasterized to PNG at `~/.cache/seek/pdf/` and embedded the same way (DashScope's multimodal API accepts `image/*`, not PDF directly)
- Embedded PDF text is indexed for keyword search; scanned pages are OCR'd when `ocr.enabled` is set (any OpenAI-compatible vision model, default `qwen-vl-ocr`)
- Text and image chunks use the same embedding model (unified vector space)
- Vector search naturally finds relevant images alongside text results

### Embedding provider config (`~/.config/seek/config.yaml`)

```yaml
embedding:
  base_url: https://api.openai.com/v1   # any OpenAI-compatible endpoint
  api_key: ${OPENAI_API_KEY}
  model: text-embedding-3-small
  dimensions: 1024
  # multimodal: true                    # enable image+text (VL) embedding
  # vl_base_url: https://...            # VL endpoint; unset → DashScope default
```

- `multimodal` is auto-detected from model names containing `vl-embedding`/`multimodal`, or forced via `multimodal: true`.
- `vl_base_url` defaults to DashScope's qwen3-vl-embedding; point it elsewhere to use a different vision-language provider.
- Changing `model` or `dimensions` requires re-indexing (`seek rm <collection>` then `seek add`).

## Important Notes

- **Do NOT run `sync` or `embed` proactively.** Only when user asks to update.
- **Always use absolute path** to the binary: `seek`
- Hybrid/vec search requires API key (already configured in `~/.config/seek/config.yaml`)
- If search returns no results, try rephrasing or switching between `--lex` and hybrid
- Multilingual queries work — the index supports mixed-language content
- Vector search uses HNSW index (persisted at `~/.cache/seek/hnsw.index`) for fast approximate nearest neighbor search; falls back to linear scan if the index is missing or corrupt
- Chunk content is compressed with Zstd by default to reduce storage; uncompressed content remains readable
- Query parsing is enabled by default; invalid syntax falls back to raw FTS5 MATCH automatically
