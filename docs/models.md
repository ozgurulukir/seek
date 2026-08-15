# Embedding & Re-ranking Models Guide

`seek` supports any **OpenAI-compatible embedding endpoint** (`POST /embeddings`) and any **standard Cross-Encoder Re-ranking endpoint** (`POST /rerank`), across both cloud providers and self-hosted/local engines.

---

## 📊 Embedding Models Comparison

| Provider / Model | Dimensions | Modality | Environment | Speed / Latency (CPU) | Code Search Support | Best For |
|---|---|---|---|---|---|---|
| **`nomic-embed-text`** (Ollama) | **768** | Text | Local (Ollama) | ~8–15 ms | **Good** (8K context window) | Simplest 1-click local & offline setup |
| **`BAAI/bge-m3`** (Local/TEI) | **1024** | Text / Multi | Local / Self-hosted | ~20–40 ms | **Exceptional** (100+ PLs & NLs) | SOTA local code, dense+sparse hybrid |
| **`text-embedding-3-small`** (OpenAI) | **1536** / 1024 | Text | Cloud API | ~60–100 ms | **High** | High accuracy general cloud embedding |
| **`qwen3-vl-embedding`** (DashScope) | **1024** | Multimodal (Text + Images) | Cloud API | ~100–180 ms | **High** | Visual search, PDFs & screenshot matching |
| **`text-embedding-v3`** (DashScope) | **1024** | Text | Cloud API | ~50–90 ms | **High** | Balanced default text embeddings |
| **`all-MiniLM-L6-v2`** (Local) | **384** | Text | Local CPU / Edge | ~2–5 ms | **Basic** | Ultra-lightweight & low-resource devices |

---

## 🧠 Re-ranking Models Comparison (Cross-Encoders)

Cross-Encoder re-ranking evaluates query + document pairs together with full attention layers, providing deep semantic relevance and a significant accuracy boost (~15–25% NDCG over pure embeddings).

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
