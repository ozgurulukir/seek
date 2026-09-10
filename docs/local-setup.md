# 100% Local & Offline Setup (Ollama + FlashRank)

`seek` can operate in a **100% private, offline, zero-cloud** mode on your local machine with zero external API calls and zero cloud subscription costs.
The strict-offline examples below use the builtin extractor. The optional xberg
service is covered separately and is not allowed while
`privacy.offline_only: true` is enabled.

---

## 🏛️ Local Architecture Flow

```
┌────────────────────────────────────────────────────────────────────────┐
│               100% Local & Offline Hybrid Search Flow                  │
└────────────────────────────────────────────────────────────────────────┘
                                    │
                                 [Query]  (e.g., "how does vector search work")
                                    │
                    ┌───────────────┴───────────────┐
                    ▼                               ▼
          ┌───────────────────┐           ┌───────────────────┐
          │  SQLite FTS5 BM25 │           │   Local Ollama    │
          │  (Keyword Match)  │           │(nomic-embed-text) │
          └─────────┬─────────┘           └─────────┬─────────┘
                    │                               │
                    │      ┌────────────────────────┘
                    │      ▼
                    │  ┌─────────────────────────┐
                    │  │   HNSW Vector Index     │
                    │  │   (Cosine Similarity)   │
                    │  └───────────┬─────────────┘
                    │              │
                    ▼              ▼
          ┌────────────────────────────────────────┐
          │     RRF (Reciprocal Rank Fusion)       │
          │         Top Candidate Pool             │
          └──────────────────┬─────────────────────┘
                             │
                             ▼
          ┌────────────────────────────────────────┐
          │       Local FlashRank Re-Ranker        │
          │       (ms-marco-TinyBERT-L-2-v2)       │
          │   Full Token Cross-Attention Scoring   │
          └──────────────────┬─────────────────────┘
                             │
                             ▼
          ┌────────────────────────────────────────┐
          │    Precision Result with Line Spans    │
          │    path/to/file.go:L25-L68 (-C ctx)    │
          └────────────────────────────────────────┘
```

---

## 🛠️ Step-by-Step Local Walkthrough

### 1. Launch Local Embedding Model

Choose either **Option A (Ollama)** or **Option B (Pure `uv` FastEmbed)**:

**Option A — Ollama:**
```bash
ollama pull nomic-embed-text
# Runs at http://127.0.0.1:11434/v1
```

**Option B — Pure `uv` FastEmbed (ONNX, Ultra-lightweight ~22MB, No Ollama needed):**
```bash
uv run tools/embed_server/server.py
# Runs at http://127.0.0.1:8002/v1 (default: all-MiniLM-L6-v2, 384 dims, ~2-5ms)
```

### 2. Launch Optional Local Helper Services (`tools/`)
The repository includes single-file PEP 723 Python server scripts that launch via `uv`:

```bash
# Terminal 1: Start FlashRank Cross-Encoder Re-ranker (Port 8000)
uv run tools/flashrank_server/server.py

# Terminal 2: Start xberg Rich Document Extractor (Port 8001, optional)
uv run tools/xberg_server/server.py
```

The xberg backend is useful for rich document conversion, but `seek` refuses
it when `privacy.offline_only: true` because the configured service may forward
document contents. Use the builtin backend for strict offline operation, or
explicitly disable offline-only mode when choosing xberg.

### 3. Optional OCR for Scanned PDFs

OCR is used only for PDF pages that do not contain an embedded text layer. The
`seek` OCR client sends an OpenAI-compatible vision request to
`POST {ocr.base_url}/chat/completions` and reads the text from
`choices[0].message.content`. See the [OCR pipeline details](extractors.md#-ocr-vision-pipeline-ocr).
The request uses `stream: false` and a configurable `ocr.max_tokens` limit
(default `2048`) so models do not run away into long commentary; increase the
limit for unusually dense pages if OCR is truncated.

`privacy.offline_only: true` now permits OCR requests only to numeric loopback
endpoints (`127.0.0.0/8` or `::1`). Remote, private-network, and hostname-based
OCR endpoints remain blocked. This limits seek's direct network destination;
seek also bypasses environment HTTP proxies for these destinations. The local
OCR server is still a trusted boundary and could forward data itself.

#### OCR compatibility table

The green check means the current `seek` OCR client can call the named provider
and model family without a provider-specific adapter. A warning means the
transport is compatible but the exact runtime, model, and prompt still need a
smoke test. Neither mark guarantees OCR quality; test your own Turkish/English
scans and tables.

| Provider + model/runtime | Direct with `seek` | Data location | Notes |
|---|---:|---|---|
| Ollama + `glm-ocr` | ✅ | Local | Best first local candidate; OCR-focused |
| Ollama + `qwen3-vl` / `gemma3` | ✅ | Local | General vision models with OCR support |
| vLLM + GLM-OCR or another supported vision model | ⚠️* | Local | Transport-compatible; exact model/chat-template smoke test required |
| llama.cpp / `llama-server` + multimodal GGUF | ⚠️* | Local | Transport-compatible; model and `mmproj` must match |
| LM Studio + a loaded vision model | ⚠️* | Local | Transport-compatible; loaded model must accept images |
| DashScope `qwen-vl-ocr` / `qwen3.5-ocr` | ✅ | Cloud | OpenAI-compatible, but not offline |
| Baidu standard OCR API | ❌ | Baidu cloud | Different auth, request, and response contract |
| PaddleOCR, RapidOCR, Tesseract | ❌ | Local | Native OCR APIs; separate integration required |

For the evidence and the distinction between direct and adapter-based options,
see the [OCR provider compatibility research](ocr-provider-compatibility.md).

`*` The warning rows are viable direct candidates only after the exact local
runtime and model pass the smoke test described in the corresponding quickstart.

#### Quickstart A — Ollama + `glm-ocr` (recommended local setup)

`glm-ocr` is an OCR-focused multimodal model. Ollama exposes it through the
OpenAI-compatible vision endpoint that `seek` already understands. The model
download requires internet access once; inference and PDF images remain local
after the model is installed. See the [official GLM-OCR model page](https://ollama.com/library/glm-ocr)
and [Ollama OpenAI compatibility docs](https://docs.ollama.com/api/openai-compatibility).

1. Install Ollama and start its local server:

   ```bash
   ollama serve
   ```

   If Ollama is already running as a system service, keep the existing service
   and skip this command.

2. Download the model:

   ```bash
   ollama pull glm-ocr
   ollama run glm-ocr
   ```

   `ollama run` is an optional sanity check. Exit it after the model loads;
   `seek` will use the same local Ollama server.

3. Configure `~/.config/seek/config.yaml`:

   ```yaml
   ocr:
     enabled: true
     base_url: http://127.0.0.1:11434/v1
     api_key: ollama       # accepted by Ollama and ignored by its local server
     model: glm-ocr
     max_tokens: 2048      # raise for unusually dense pages

   privacy:
     offline_only: true   # loopback OCR is allowed; external OCR is blocked
   ```

4. Index a scanned PDF and search its extracted text:

   ```bash
   seek doctor
   seek add --pdf /path/to/scanned-pdfs --name scans
   seek sync
   seek search "aranacak metin" --lex
   ```

   OCR is called only for pages without an embedded text layer. `--lex` is a
   useful first check because OCR output is written into the FTS5 index.

5. If the result is empty, check the following before changing code:

   - `ollama list` shows `glm-ocr` and `ollama serve` is reachable.
   - `ocr.base_url` ends at `/v1`; `seek` appends `/chat/completions`.
   - The PDF page is actually scanned; text-native pages do not invoke OCR.
   - The configured endpoint uses a numeric loopback address when `offline_only` is true.

`seek` sends a generic “extract all text verbatim” prompt. GLM-OCR’s official
examples also include task-specific prompts for text, tables, and figures, so
layout-heavy documents should be evaluated with representative fixtures before
making it the default for a large collection.

#### Quickstart B — Ollama + `qwen3-vl` or `gemma3`

Use this when you already run Qwen3-VL or want a general vision model. Ollama’s
official example uses `qwen3-vl:8b` with the same Base64 image format:

```bash
ollama serve
ollama pull qwen3-vl:8b
# Alternative general vision model:
# ollama pull gemma3:4b
```

```yaml
ocr:
  enabled: true
  base_url: http://127.0.0.1:11434/v1
  api_key: ollama
  model: qwen3-vl:8b       # or gemma3:4b

privacy:
  offline_only: true
```

Then run `seek sync`. Smaller variants use less memory; larger variants may
improve small-text recognition. Compare them on the same scanned pages rather
than assuming the larger model is always better.

#### Quickstart C — vLLM + GLM-OCR or another supported vision model

This option is intended for a GPU-backed local inference server. vLLM must be
started with a model that supports multimodal chat input and has a compatible
chat template. GLM-OCR’s official repository documents an OpenAI-compatible
self-hosted service:

```bash
vllm serve zai-org/GLM-OCR \
  --port 8080 \
  --served-model-name glm-ocr
```

```yaml
ocr:
  enabled: true
  base_url: http://127.0.0.1:8080/v1
  api_key: local
  model: glm-ocr

privacy:
  offline_only: true
```

See the [GLM-OCR self-hosting instructions](https://github.com/zai-org/GLM-OCR)
and [vLLM multimodal serving docs](https://docs.vllm.ai/en/latest/features/multimodal_inputs/).

#### Quickstart D — llama.cpp / `llama-server`

Use a multimodal GGUF model together with its matching multimodal projector.
The model and projector are model-specific; this is a transport-compatible
setup, not a promise that every GGUF has OCR quality.

```bash
llama-server \
  -m /path/to/model.gguf \
  --mmproj /path/to/mmproj.gguf \
  --port 8080
```

```yaml
ocr:
  enabled: true
  base_url: http://127.0.0.1:8080/v1
  api_key: local
  model: local-vision

privacy:
  offline_only: true
```

See the [llama.cpp multimodal guide](https://github.com/ggml-org/llama.cpp/blob/master/docs/multimodal.md)
for supported model-specific input formats.

#### Quickstart E — LM Studio + a local vision model

Load a vision-capable model in LM Studio, start its local server, and expose
the OpenAI-compatible API on port `1234`:

```yaml
ocr:
  enabled: true
  base_url: http://127.0.0.1:1234/v1
  api_key: lm-studio
  model: <loaded-vision-model-id>

privacy:
  offline_only: true
```

The loaded model must support image input. See [LM Studio OpenAI compatibility](https://lmstudio.ai/docs/developer/openai-compat)
for the local server settings.

#### Quickstart F — DashScope Qwen-OCR (direct, but cloud)

This is the simplest hosted option. It is directly compatible with `seek`, but
`offline_only` must remain false because scanned page images are sent to
DashScope:

```yaml
ocr:
  enabled: true
  base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
  api_key: ${DASHSCOPE_API_KEY}
  model: qwen-vl-ocr

privacy:
  offline_only: false
```

See the [official Qwen-OCR API reference](https://help.aliyun.com/en/model-studio/qwen-vl-ocr-api-reference)
for supported model names and input limits.

#### Baidu OCR (cloud, adapter required)

Baidu OCR is a hosted service, not a local model. Its standard OCR endpoint
uses `POST /rest/2.0/ocr/v1/general_basic`, an `access_token`, and an
`application/x-www-form-urlencoded` body containing the image as URL-encoded
Base64. It does not implement the OpenAI-compatible request/response contract
that `seek` currently uses. Refer to Baidu's [official general OCR API
documentation](https://ai.baidu.com/ai-doc/OCR/zk3h7xz52) and [access-token
documentation](https://ai.baidu.com/ai-doc/REFERENCE/Ck3dwjhhu).

To use Baidu without embedding provider-specific code in `seek`, run an
adapter locally. The adapter should:

1. Accept `POST /v1/chat/completions` in the format sent by `seek`.
2. Convert the `image_url` data URI to Base64 and call Baidu's OCR endpoint.
3. Convert Baidu's `words_result[].words` response into
   `choices[0].message.content`.

Then configure `seek` to use the adapter, not Baidu's OCR URL directly:

```yaml
ocr:
  enabled: true
  base_url: http://127.0.0.1:8010/v1
  api_key: local-adapter-token
  model: baidu-general-basic
```

Because the adapter forwards document images to Baidu, this setup is **not
offline**. Keep `offline_only: false` and treat the Baidu API key/access token
as a secret. Native Baidu OCR support is not currently included in `seek`.
For the request/response comparison and Baidu input limits, see the
[Baidu OCR support note](baidu-ocr.md).

### 4. Configure `~/.config/seek/config.yaml`

**For Option A (Ollama - Nomic 768 dims):**
```yaml
embedding:
  base_url: http://127.0.0.1:11434/v1
  api_key: ollama
  model: nomic-embed-text
  dimensions: 768
```

**For Option B (Pure `uv` FastEmbed - MiniLM 384 dims):**
```yaml
embedding:
  base_url: http://127.0.0.1:8002/v1
  api_key: local
  model: sentence-transformers/all-MiniLM-L6-v2
  dimensions: 384
```

```yaml
rerank:
  enabled: true
  base_url: http://127.0.0.1:8000
  api_key: local
  model: ms-marco-TinyBERT-L-2-v2
  top_n: 10

# Query analysis/stemming language ("en" or "tr"). The --analyze-lang /
# -l CLI flags override this per invocation. Turkish stemming guards ASCII
# technical words (launchd, config, sqlite) from corruption.
search:
  analyze_lang: tr

extractor:
  backend: builtin
  xberg_base_url: http://127.0.0.1:8001
  output_format: markdown
```

### 5. Index, Embed, and Search
```bash
# Add source code and notes with the builtin extractor
seek add ~/notes --name mynotes            # markdown
seek add ~/projects/myrepo --code
seek add ~/docs --documents --name docs     # builtin-supported documents

# Rich document conversion via xberg is optional; it requires
# extractor.backend: xberg and privacy.offline_only: false.

# Sync and embed locally
seek sync
seek embed -f -r

# Execute local hybrid search with context expansion
seek search "how does vector search work" -C 1
```

---

## 🚀 Standalone Helper Scripts Reference

- **`tools/flashrank_server/server.py`**:
  Lightweight ONNX-powered Cross-Encoder REST API providing `POST /rerank` and `GET /health` on port `8000`. Compatible with any standard reranker client protocol.
- **`tools/xberg_server/server.py`**:
  Multi-format document conversion API providing `POST /extract`, `GET /formats`, and `GET /health` on port `8001`. Handles `.docx`, `.xlsx`, `.pdf`, `.html`, and `.csv`.
