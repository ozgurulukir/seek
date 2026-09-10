# OCR Reference

Use this reference when a user wants to OCR scanned PDFs, configure a local
vision model, or understand `privacy.offline_only` behavior.

## What seek sends

For each PDF page without embedded text, seek sends one OpenAI-compatible
vision request to:

```text
POST {ocr.base_url}/chat/completions
Authorization: Bearer <ocr.api_key>
```

The image is a `data:image/png;base64,...` URL inside
`messages[].content[].image_url.url`. The request is non-streaming and includes
`max_tokens` (default `2048`). The extracted text must be in
`choices[0].message.content`.

`ocr.base_url`, `ocr.api_key`, and `ocr.model` fall back to the embedding
provider when omitted. `ocr.max_tokens` controls the response limit; increase
it for unusually dense pages.

## Strict offline mode

With `privacy.offline_only: true`, seek constructs an OCR client only for an
HTTP(S) URL using a numeric loopback address:

- IPv4: `127.0.0.0/8` (for example `127.0.0.1`)
- IPv6: `::1` (for example `http://[::1]:11434/v1`)

Hostnames, including `localhost`, plus private-network and remote addresses are
refused. Numeric loopback OCR requests bypass environment HTTP proxies. This
policy limits seek's direct destination; the local OCR server is a trusted
boundary and could still forward document images itself.

Embedding and reranking remain disabled by `offline_only`; OCR is the explicit
local-loopback exception. Keyword search (`--lex`) remains local.

## Quickstart: Ollama + `glm-ocr`

This is the recommended first local candidate. Downloading the model needs
network access once; inference and scanned page images stay on the local
Ollama server after installation.

```bash
# Terminal 1
ollama serve

# Terminal 2
ollama pull glm-ocr
ollama run glm-ocr       # optional model-load sanity check; exit afterwards
```

Configure `~/.config/seek/config.yaml`:

```yaml
ocr:
  enabled: true
  base_url: http://127.0.0.1:11434/v1
  api_key: ollama
  model: glm-ocr
  max_tokens: 2048

privacy:
  offline_only: true
```

Then index and verify a scanned PDF:

```bash
seek doctor
seek add /path/to/scanned-pdfs --pdf --name scans
seek sync
seek search "search text" --lex
```

If OCR is empty, first confirm that the PDF is image-only, `glm-ocr` appears in
`ollama list`, the server is reachable, and the configured URL is numeric
loopback. If a dense page is cut off, increase `ocr.max_tokens`.

## Compatibility

The marks describe integration confidence, not OCR quality:

| Provider/runtime | Status | Meaning |
|---|---:|---|
| Ollama + `glm-ocr` | ✅ | Recommended OCR-focused local candidate |
| Ollama + `qwen3-vl` / `gemma3` | ✅ | Direct OpenAI-compatible vision path; test quality |
| DashScope `qwen-vl-ocr` / `qwen3.5-ocr` | ✅ | Direct, but cloud; do not use with strict offline mode |
| vLLM + a supported vision/OCR model | ⚠️ | Transport-compatible; exact model/template smoke test required |
| llama.cpp + multimodal GGUF | ⚠️ | Transport-compatible; matching `mmproj` and model test required |
| LM Studio + a loaded vision model | ⚠️ | Transport-compatible; loaded model must accept images |
| Baidu standard OCR API | ❌ | Different auth, body, endpoint, and response contract |
| PaddleOCR, RapidOCR, Tesseract | ❌ | Native OCR APIs; separate integration required |

For human-facing setup details and the research rationale, see the repository's
[`docs/local-setup.md`](../../../docs/local-setup.md) and
[`docs/ocr-provider-compatibility.md`](../../../docs/ocr-provider-compatibility.md).
