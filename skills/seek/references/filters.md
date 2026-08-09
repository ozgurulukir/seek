# Filters

Filters work with both `--lex` and `--vec` modes.

## Collection

```bash
seek search "query" --collection mynotes
```

## Document Type

```bash
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

## Combining Filters

Multiple filters can be combined:

```bash
seek search "query" --collection mynotes --doc-type markdown --after 2024-01-01
```
