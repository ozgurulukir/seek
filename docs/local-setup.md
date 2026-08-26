# 100% Local & Offline Setup (Ollama + FlashRank + xberg)

`seek` can operate in a **100% private, offline, zero-cloud** mode on your local machine with zero external API calls and zero cloud subscription costs.

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
# Runs at http://localhost:11434/v1
```

**Option B — Pure `uv` FastEmbed (ONNX, Ultra-lightweight ~22MB, No Ollama needed):**
```bash
uv run tools/embed_server/server.py
# Runs at http://127.0.0.1:8002/v1 (default: all-MiniLM-L6-v2, 384 dims, ~2-5ms)
```

### 2. Launch Local Helper Services (`tools/`)
The repository includes single-file PEP 723 Python server scripts that launch via `uv`:

```bash
# Terminal 1: Start FlashRank Cross-Encoder Re-ranker (Port 8000)
uv run tools/flashrank_server/server.py

# Terminal 2: Start xberg Rich Document Extractor (Port 8001)
uv run tools/xberg_server/server.py
```

### 3. Configure `~/.config/seek/config.yaml`

**For Option A (Ollama - Nomic 768 dims):**
```yaml
embedding:
  base_url: http://localhost:11434/v1
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

rerank:
  enabled: true
  base_url: http://127.0.0.1:8000
  api_key: local
  model: ms-marco-TinyBERT-L-2-v2
  top_n: 10

extractor:
  backend: xberg
  xberg_base_url: http://127.0.0.1:8001
  output_format: markdown
```

### 4. Index, Embed, and Search
```bash
# Add source code, notes, and rich documents
seek add ~/projects/myrepo --code
seek add ~/docs --docs

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
