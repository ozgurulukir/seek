# Troubleshooting

## No results returned

1. Try rephrasing the query
2. Switch between `--lex` and hybrid mode
3. Check if the collection has documents: `seek status`
4. Run `seek sync` to ensure index is up to date

## Hybrid/vector search fails

- Vector search requires an embedding API key
- Check `~/.config/seek/config.yaml` for `embedding.api_key`
- If missing, add the key and run `seek embed`

## Index seems stale

```bash
seek sync    # incremental sync
seek embed   # generate embeddings for new chunks
```

## Force re-embed

After changing model, dimensions, or task prefixes:

```bash
seek embed -f
```

## Collection not found

```bash
seek status                  # list all collections
seek parsers list            # list parser schemas and detection status
```

## Binary not found

Ensure `seek` is in PATH:

```bash
# Linux / macOS:
which seek

# Windows (PowerShell):
Get-Command seek
```

If not found, copy or move the binary to a directory in your PATH (e.g. `~/.local/bin/` on POSIX or `$env:USERPROFILE\go\bin` on Windows).
