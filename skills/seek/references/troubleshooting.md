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

## Scanned PDF OCR is empty or fails

- OCR is called only when a PDF page has no embedded text layer. Text-native PDF
  pages do not call the OCR provider.
- Confirm `ocr.enabled: true`, a non-empty `ocr.api_key`, and the correct model
  endpoint. For Ollama, use `http://127.0.0.1:11434/v1`, `api_key: ollama`, and
  `model: glm-ocr`.
- With `privacy.offline_only: true`, use a numeric loopback URL (`127.0.0.0/8`
  or `::1`). `localhost`, private-network addresses, and remote endpoints are
  refused. The local OCR server itself must be trusted not to forward images.
- Run `seek doctor` to inspect the OCR destination and policy status, then run
  `seek sync` and verify with `seek search "text" --lex`.
- If output is truncated, raise `ocr.max_tokens` above its default of `2048`.
- If the server rejects the request, verify that `ocr.base_url` is the API root
  (for example `/v1`); seek normalizes a trailing slash and appends
  `/chat/completions`.

## Index seems stale

```bash
seek sync    # incremental sync
seek embed   # generate embeddings for new chunks
```

## Force re-embed & Re-indexing

- **After changing model or task prefixes:**
  ```bash
  seek embed -f
  ```
- **After changing chunk size (`chunk.max_size`) or dimensions:**
  Re-index collection so files are sliced into new chunk boundaries:
  ```bash
  seek rm <collection>
  seek add <path> [--code|--documents|...]
  seek embed
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
