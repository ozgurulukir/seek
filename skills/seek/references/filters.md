# Filters

Filters work with both `--lex` and `--vec` modes.

## Repository / Collection

Filter results to a specific repository or collection name (`--repo` is an alias for `--collection`):

```bash
seek search "query" --repo myproject
seek search "query" --collection mynotes
```

## Programming Language

Filter source code results by programming language (e.g. `go`, `python`, `typescript`, `rust`, `c`, `cpp`, `java`):

```bash
seek search "func Open" --lang go
seek search "import React" --lang typescript
```

## Document Type

Document types: `code`, `markdown`, `claude`, `codex`, `images`, `pdf`, `documents`, `parser`.

```bash
seek search "query" --doc-type code
seek search "query" --doc-type markdown
```

## Date Range

```bash
seek search "query" --after 2024-01-01 --before 2024-12-31
```

## Chunk Type

```bash
seek search "query" --chunk-type image
```

## Path

```bash
seek search "query" --path "docs/*.md"
```

## Workspace

Parser collections only:

```bash
seek search "query" --workspace /path/to/project
```

## Context Window Expansion

Expand surrounding chunk context before and after matching hits:

```bash
seek search "query" -C 1        # 1 chunk before and after
seek search "query" --context 2  # 2 chunks before and after
```

## Sorting Results

By default, results are sorted by hybrid score (BM25 + RRF + reranker if enabled). You can override this:

```bash
# Sort by document creation time (newest first)
seek search "query" --sort-by created_at

# Sort by creation time ascending (oldest first)
seek search "query" --sort-by created_at --sort-order asc

# Sort by line count (larger documents first)
seek search "query" --sort-by line_count

# Sort by line count ascending
seek search "query" --sort-by line_count --sort-order asc
```

**Sort fields:**
- `created_at` — document creation timestamp
- `line_count` — document line count

**Sort order:**
- `desc` — descending (default, highest/newest first)
- `asc` — ascending (lowest/oldest first)

## Query Mode

The query parser is enabled by default, supporting boolean, phrase, fuzzy, and field-scoped syntax. Invalid syntax automatically falls back to raw FTS5 MATCH.

```bash
# Force raw mode (bypass structured query parser entirely)
seek search "complex AND syntax OR that might break" --query-mode raw

# Explicitly request parsed mode (default)
seek search "term1 AND term2" --query-mode parsed
```

**When to use raw mode:**
- Your query contains special characters that confuse the parser
- You want to search for literal `AND`, `OR`, `NOT` words
- You want direct FTS5 MATCH syntax without any preprocessing

## Combining Filters

Multiple filters and options can be combined:

```bash
seek search "query" \
  --repo myproject \
  --lang go \
  -C 1 \
  --after 2024-01-01 \
  --sort-by created_at \
  --sort-order desc \
  -l 20
```

