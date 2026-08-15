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

## Combining Filters

Multiple filters can be combined:

```bash
seek search "query" --repo myproject --lang go -C 1 --after 2024-01-01
```

