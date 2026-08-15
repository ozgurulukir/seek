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

# Time-based histograms
seek search "release" --aggs "created_at:histogram:month"

# Numeric range buckets
seek search "func" --aggs "line_count:range:0-50,50-200,200+"
```
