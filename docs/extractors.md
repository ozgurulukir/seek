# Document Extractors & OCR Guide

`seek` supports flexible document extraction backends to transform diverse file formats into searchable chunks and vectors.

---

## 📂 Extraction Backends

`seek` provides two extraction engines:

1. **`builtin` Backend (Default):**
   - **Markdown (`.md`):** Content-hash incremental sync with structural header chunking.
   - **Source Code (`--code`):** 35+ languages, `.gitignore`-aware scanning, structural block and line-based sliding window chunking.
   - **PDFs (`--pdf`):** Rasterizes pages to PNG via `go-fitz` for vision-language (VL) embedding; extracts embedded text for keyword search.
   - **Images (`--images`):** `.png`, `.jpg`, `.webp` indexed directly for multimodal embedding.

2. **`xberg` Backend (`--docs`):**
   - Universal converter for 100+ rich document formats:
     - Office documents: `.docx`, `.xlsx`, `.pptx`
     - E-books & Web: `.epub`, `.html`, `.htm`
     - Data: `.csv`, `.tsv`, `.eml`
   - Communicates with an `xberg serve` API endpoint via standard multipart uploads (`POST /extract`).

### Using `xberg` with `seek`:

```bash
# 1. Start local xberg server on port 8001
uv run tools/xberg_server/server.py

# 2. Add collection with --docs
seek add /path/to/documents --docs --name papers

# 3. Sync
seek sync
```

---

## 👁️ OCR Vision Pipeline (`ocr:`)

For scanned PDF documents without embedded text layers:

```yaml
# In ~/.config/seek/config.yaml:
ocr:
  enabled: true
  # Defaults to embedding provider if unset:
  # base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
  # api_key: ${DASHSCOPE_API_KEY}
  # model: qwen-vl-ocr
  # max_tokens: 2048
```

- When a PDF page contains no embedded text, `seek` invokes the configured vision/OCR model during `seek sync`.
- The request is non-streaming and defaults to a 2048-token response limit; set `ocr.max_tokens` higher for unusually dense pages if the result is truncated.
- Extracted text is indexed into FTS5 and chunk content, making scanned documents fully keyword- and hybrid-searchable.
- With `privacy.offline_only: true`, OCR is allowed only for numeric loopback endpoints (`127.0.0.0/8` or `::1`), and environment HTTP proxies are bypassed. This limits seek's direct destination; trust the local OCR server not to forward document images. See the [local OCR quickstarts](local-setup.md#3-optional-ocr-for-scanned-pdfs).
