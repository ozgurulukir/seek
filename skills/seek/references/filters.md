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

## Fast-field filter (`--field`)

The generic `--field <name>:<value>` flag filters by any fast field. Match
semantics depend on the field type:

- **Exact** (single-value fields): `lang`, `ext`, `filename`, `rel_path`,
  `repo`, `workspace`, `language`, plus the parserdef conversation-context
  fields `parent`, `platform`, `profile`, `channel`, `model`.
- **Comma-list membership** (multi-value fields): `tags`, `topics`,
  `entities` — a whole comma-separated token matches (substrings do not).

Field names are not limited to this curated list: every field name physically
present in the index is accepted in exact mode (e.g. arbitrary markdown
frontmatter keys such as `author`). Run `seek fields` to discover what the
index holds; curated fields are listed first, dynamically indexed fields
after, sorted alphabetically.

```bash
seek search "gradient" --field tags:go        # tags list contains "go" (not "golang")
seek search "note" --field tags:priority      # tags fast field (frontmatter + semantic)
seek search "rust" --field "topics:ownership and compile free"  # full topic token
seek search "sql"  --field language:en        # exact
seek search "x"    --field repo:myproject --field lang:go
```

`tags` comes from markdown YAML frontmatter or the optional semantic tag
service; all tagged sources are facetable with `--aggs tags:terms`. The old
`--tag` flag was removed — `--field tags:<value>` gives identical behaviour.

The same acceptance rule as `--field` applies to `--aggs <field>:terms`:
curated fields plus every field physically present in the index (exact
whole-value buckets). Histograms and ranges remain documents-column only.

## Semantic enrichment fields

For pdf / documents / conversation collections, the optional local semantic
tag service (see [docs/semantic.md](docs/semantic.md)) adds three more fast
fields alongside `tags`. All are facetable with `--aggs <field>:terms` and
filterable with `--field`:

```bash
seek search "lang" --aggs topics:terms             # topics
seek search "rust" --field "topics:ownership and compile free"
seek search "file" --aggs language:terms           # language
seek search "x"    --field language:en
```

These fields exist only when `semantic.enabled: true`; without the service
they are absent (no error, no empty facets).

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

