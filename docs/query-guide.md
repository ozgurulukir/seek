# Search Query Syntax, Filters & Line Addressing Guide

`seek` features a rich query engine supporting BM25 full-text, HNSW vector search, RRF fusion, Cross-Encoder re-ranking, and structured AST query parsing.

---

## 🎯 Search Modes

| Mode | Flag | Ranking | Description |
|---|---|---|---|
| **Hybrid (Default)** | *(no flag)* | RRF + Re-rank | Combines BM25 and vector semantic search via Reciprocal Rank Fusion + optional Cross-Encoder. |
| **Keyword Only** | `--lex` | BM25 | Pure full-text search with Turkish/Unicode diacritics folding (`remove_diacritics 2`). Fast, 100% offline. |
| **Semantic Only** | `--vec` | Cosine Sim | Vector-only semantic search using HNSW index. |

---

## 🔍 Structured Query Syntax

By default (`search.query_mode: parsed`), queries are parsed into an AST. If syntax is invalid, it falls back to raw FTS5 MATCH automatically.

- **Boolean Operators:**
  `authentication AND middleware`
  `react OR vue`
  `NOT deprecated`
  `(postgres OR sqlite) AND "connection pool"`
- **Exact Phrases:** `"deploy the gateway"`
- **Prefix Matching:** `handl*` (matches `handle`, `handler`, `handling`)
- **Field-Scoped Queries:** `title:migration`, `content:sql`
- **Proximity:** `NEAR(docker compose, 3)`
- **Fuzzy:** `service~2` (maps to prefix expansion)

---

## 🎛️ Search Filters

Narrow search results by collection type, language, date, or filesystem path:

```bash
# Filter by collection or repository
seek search "handleRequest" --repo seek
seek search "meeting notes" --collection mynotes

# Filter by programming language (for code collections)
seek search "Open" --lang go
seek search "useEffect" --lang typescript

# Filter by document type
seek search "architecture" --doc-type markdown
seek search "trace" --doc-type code

# Filter by file path pattern (GLOB syntax)
seek search "Client" --path "*extractor*"
seek search "test" --path "internal/store/*"

# Filter by date range (RFC3339)
seek search "summary" --after 2026-01-01 --before 2026-12-31

# Filter by chunk type (text vs extracted screenshot images)
seek search "layout error" --chunk-type image
```

---

## 🔀 Sorting by Metadata Fields (`--sort-by`, `--sort-order`)

By default, search results are ordered by hybrid relevance score (or cross-encoder score if configured). You can explicitly sort results by any indexed fast-field:

```bash
# Sort by creation / modification time
seek search "deploy" --sort-by created_at --sort-order desc

# Sort alphabetically by title
seek search "architecture" --sort-by title --sort-order asc

# Sort by line count / document size
seek search "parser" --sort-by line_count --sort-order desc
```

---

## 📍 Precision Source Addressing & Context Expansion (`-C`)

### 1. Precise 1-Based Line Spans
Search outputs exact 1-based start and end line ranges (`path/to/file.go:L25-L68`), enabling immediate IDE and AI agent navigation.

### 2. Surrounding Context Expansion (`-C` / `--context`)
Pass `-C <radius>` to expand adjacent chunk text and compute expanded line numbers:
```bash
# Expands 1 chunk before and 1 chunk after match hits
seek search "InsertChunkWithLines" -C 1 -l 3
```

---

## 📊 Faceted Search & Aggregations (`--aggs`)

Compute statistical facet distributions alongside search results:

```bash
# Distribution by collection and document type
seek search "error" --aggs "type:terms" --aggs "collection:terms"

# Metadata fast-field facets (markdown frontmatter + code metadata)
seek search "signal" --aggs "tags:terms"        # tags (frontmatter + semantic)
seek search "error" --aggs "lang:terms"          # code language (go, rust, python, ts, …)
seek search "req"   --aggs "repo:terms"          # repository/collection name

# Semantic enrichment facets (pdf/documents/conversations, semantic.enabled)
seek search "note"  --aggs "topics:terms"        # BERTopic labels
seek search "rust"  --aggs "entities:terms"      # "TYPE:Text" NER pairs
seek search "x"     --aggs "language:terms"      # detected ISO 639-1 (e.g. "en")

# Time-based histograms
seek search "release" --aggs "created_at:histogram:month"

# Numeric range buckets
seek search "func" --aggs "line_count:range:0-50,50-200,200+"
```

The `tags`, `lang`, `repo`, `ext`, `filename`, `rel_path`, `topics`,
`entities`, and `language` facets come from the fast-field metadata written
at index time: markdown notes expose their YAML frontmatter keys (e.g.
`tags`, `date`), code files expose language and repository, and the optional
semantic service (see [docs/semantic.md](semantic.md)) adds `topics`,
`entities` and `language` for pdf/documents/conversations. They are
filterable too via the generic `--field <name>:<value>` flag (exact for
single-value fields, comma-list membership for `tags`/`topics`/`entities`):

```bash
# Filter to documents tagged in frontmatter / semantic tags
seek search "gradient" --field tags:go

# Semantic enrichment fields
seek search "rust" --field "topics:ownership"          # full topic label token
seek search "sql"  --field language:en

# Combined filters
seek search "error" --lang go --repo myrepo
```
