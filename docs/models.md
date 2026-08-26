# Embedding & Re-ranking Models Guide

`seek` supports any **OpenAI-compatible embedding endpoint** (`POST /embeddings`) and any **standard Cross-Encoder Re-ranking endpoint** (`POST /rerank`), across both cloud providers and self-hosted/local engines.

---

## 📊 Embedding Models Comparison

Some embedding models are **asymmetric**: they expect search queries and indexed documents to be marked differently (via text prefixes or API request fields). The **Query/Doc Prep** column shows what `seek` sends; models marked ⚠️ get the prep automatically (or via `task_prefix` config) — see [Task Prefixes](#️-task-prefixes-asymmetric-embedding-models).

| Provider / Model | Dimensions | Modality | Environment | Speed / Latency (CPU) | Code Search Support | Query/Doc Prep | Best For |
|---|---|---|---|---|---|---|---|
| **`nomic-embed-text`** (Ollama) | **768** | Text | Local (Ollama) | ~8–15 ms | **Good** (8K context window) | ⚠️ `search_query:` / `search_document:` (auto) | Simplest 1-click local & offline setup |
| **`BAAI/bge-m3`** (Local/TEI) | **1024** | Text / Multi | Local / Self-hosted | ~20–40 ms | **Exceptional** (100+ PLs & NLs) | None needed | SOTA local code, dense+sparse hybrid |
| **`text-embedding-3-small`** (OpenAI) | **1536** / 1024 | Text | Cloud API | ~60–100 ms | **High** | None needed | High accuracy general cloud embedding |
| **`text-embedding-v4`** (DashScope, default) | **1024** | Text | Cloud API | ~50–90 ms | **High** | Optional `Instruct:` gain on short queries¹ | Balanced default text embeddings |
| **`text-embedding-v3`** (DashScope) | **1024** | Text | Cloud API | ~50–90 ms | **High** | None needed | Balanced text embeddings |
| **`qwen3-vl-embedding`** (DashScope) | **1024** | Multimodal (Text + Images) | Cloud API | ~100–180 ms | **High** | None needed | Visual search, PDFs & screenshot matching |
| **`all-MiniLM-L6-v2`** (Local) | **384** | Text | Local CPU / Edge | ~2–5 ms | **Basic** | None needed (symmetric) | Ultra-lightweight & low-resource devices |

¹ `text-embedding-v4` is Qwen3-Embedding based and works robustly without instructions; a retrieval instruction can improve short queries. Set it manually via `embedding.task_prefix.query` if you want it.

> [!WARNING]
> **Nomic Embeddings**: `nomic-embed-text` expects `search_query: ` / `search_document: ` input prefixes for retrieval. The Nomic Python SDK adds them automatically via its `task_type` parameter, but **OpenAI-compatible endpoints (including Ollama, vLLM, and Nomic's hosted `/v1/embeddings`) do not**. `seek` detects `nomic-embed*` model names and prepends the prefixes automatically (queries via `seek search`, documents via `seek embed`). If you indexed before this behavior existed, re-embed once: `seek rm <collection> && seek add && seek embed -f`.

### Other models usable via custom OpenAI-compatible endpoints

| Model family | Query/Doc Prep | Method | seek behavior |
|---|---|---|---|
| E5 (`intfloat/e5-*`, `multilingual-e5-*`) | `query: ` / `passage: ` | text prefix | Auto-detected from model name |
| BGE v1 English (`bge-*-en`) | retrieval instruction on queries | text prefix | Auto-detected from model name |
| Qwen3-Embedding (self-hosted), e5-mistral | `Instruct: {task}\nQuery: …` | instruction template | Set `task_prefix.query` manually |
| GritLM | `<gct> Query: …` / `<gct> Passage: …` | text template | Set `task_prefix` manually |
| Cohere embed-v3/v4, Voyage, Jina v3, Gemini embedding | `input_type` / `task` / `task_type` | **API request field** | Not supported through OpenAI-compatible mode — prefix text is not interchangeable with these request fields |
| GTE, Snowflake arctic-embed | None needed | — | Good prefix-free local alternatives to Nomic |

---

## ⚠️ Task Prefixes (Asymmetric Embedding Models)

Some models are trained to distinguish *queries* from *documents*. When configured, `seek` prepends the **query prefix** to every `seek search` query before embedding it, and the **document prefix** to every chunk during `seek embed` (realtime, Batch API, and multimodal VL paths alike).

```yaml
embedding:
  model: nomic-embed-text        # prefixes auto-detected, nothing more to do
  # ... or override / opt in explicitly:
  task_prefix:
    query: "search_query: "      # prepended to search queries
    document: "search_document: " # prepended to indexed chunks
    # disable_auto_detect: true   # keep ONLY the explicit prefixes above —
    #                                use when the provider adds prefixes
    #                                server-side, or for family lookalikes
    #                                that are instruction-free (e.g. bge-en-icl)
```

Rules:

- **Auto-detection**: empty fields are inferred from well-known model families — `nomic-embed*` → `search_query:`/`search_document:`, `*e5*` → `query:`/`passage:`, `bge-*-en` (v1) → retrieval instruction on queries only. `bge-m3`, OpenAI, DashScope text models, GTE, and Snowflake need no prefixes. Set `disable_auto_detect: true` to opt out entirely.
- **Manual override wins field-by-field**: you can set only `query` and leave `document` to auto-detection.
- **Re-embed after changing prefixes**: prefixes change the meaning of vectors. Run `seek rm <collection>`, `seek add`, and `seek embed -f` after enabling or changing them — mixing old document vectors with new query vectors degrades recall.
- Rerankers are unaffected: cross-encoders receive the raw query and document together.

---

## 🧠 Re-ranking Models Comparison (Cross-Encoders)

Cross-Encoder re-ranking evaluates query + document pairs together with full attention layers, providing deep semantic relevance and a significant accuracy boost (~15–25% NDCG over pure embeddings). Re-ranking does **not** use task prefixes — the raw query and candidate texts are sent as-is.

| Model | Parameters | Size (Disk / RAM) | Environment | Latency (CPU) | Code Search Support | Best For |
|---|---|---|---|---|---|---|
| **`bge-reranker-v2-m3`** | ~560M | ~560 MB (ONNX) / ~1.1 GB | Local GPU / Fast CPU | ~35–80 ms | **Exceptional** (100+ PLs & NLs) | SOTA multi-language & source code search |
| **`jina-reranker-v2-base`** | ~278M | ~300 MB / API | Local / Cloud API | ~30–70 ms | **High** (Code + 8K Context) | Large function bodies, repo search & API matching |
| **`bge-reranker-small`** | ~24M–45M | ~25 MB (ONNX) / ~90 MB | Local CPU (FlashRank/TEI) | ~5–15 ms | **Good** (General code + text) | Ultra-fast local desktop search, zero lag, private |
| **`bge-reranker-large`** | ~330M–560M | ~300 MB (Quant) / ~1.3 GB | Local GPU / Cloud | ~50–150 ms | **Very High** | High-precision multi-lingual & technical docs |
| **`ms-marco-MiniLM-L-12-v2`** | ~33M | ~30 MB (ONNX) / ~120 MB | Local CPU (FlashRank) | ~8–20 ms | **Basic** (Text prioritized) | Lightweight local keyword & English text ranking |
| **`cohere-rerank-v3.5`** | Cloud API | Remote API | Cloud Endpoint | ~100–250 ms | **High** (Code & Docs) | Zero local compute, large context windows |

---

## ⚠️ Vector Dimensions Matching Rule

> [!IMPORTANT]
> The `dimensions` setting in `config.yaml` **must exactly match** the output dimension of your chosen embedding model (e.g. `1536` for OpenAI small, `768` for Nomic, `1024` for BGE-M3 / DashScope, `384` for MiniLM).
>
> Dimensions are fixed per chunk at index time and define the HNSW vector index layout. If you switch to a model with a different dimension, run `seek rm <collection>`, `seek add`, and `seek embed -f` to rebuild the vector index.
