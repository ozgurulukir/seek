# Query Syntax

Structured queries are parsed by default. Invalid syntax falls back to raw FTS5 MATCH automatically.

## Boolean

```bash
seek search "term1 AND term2"
seek search "term1 OR term2"
seek search "NOT term"
```

## Phrase

```bash
seek search '"exact phrase"'
```

## Prefix

```bash
seek search "pref*"
```

## Fuzzy

Maps to prefix expansion:

```bash
seek search "term~2"
```

## Field-scoped

```bash
seek search "title:term"
seek search "content:term"
```

## Proximity

```bash
seek search "NEAR(term1 term2, 5)"
```

## Aggregations

```bash
seek search "query" --aggs "type:terms"
seek search "query" --aggs "collection:terms"
seek search "query" --aggs "created_at:histogram:month"
seek search "query" --aggs "line_count:range:0-100,100-500"
seek search "query" --aggs "count"
```

## Autocomplete

```bash
seek search "hel" --autocomplete
```

## Text Analysis

```bash
seek analyze "running" --lang en    # English stemming: [run]
seek analyze "kitaplar" --lang tr   # Turkish stemming: [kitap]
```

## Schema

```bash
seek schema --show      # Show field types and options
seek schema --validate  # Validate schema against DB
```
