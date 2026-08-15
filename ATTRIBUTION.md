# Attributions and Third-Party Licenses

`seek` is built on top of and inspired by incredible open-source projects, models, and libraries. We gratefully acknowledge the following works:

---

## Models & Re-ranking Engines

### FlashRank
- **Project:** [FlashRank](https://github.com/Prithivida/FlashRank)
- **Author:** Prithviraj Damodaran
- **License:** Apache License 2.0
- **Description:** Ultra-lite, super-fast 100% local text re-ranking engine based on ONNX Runtime and quantized Cross-Encoders.

### BAAI FlagEmbedding (BGE)
- **Project:** [FlagEmbedding / BGE](https://github.com/FlagOpen/FlagEmbedding)
- **Authors:** Beijing Academy of Artificial Intelligence (BAAI)
- **License:** MIT License
- **Description:** State-of-the-art embedding and cross-encoder models (`bge-reranker-small`, `bge-reranker-large`, `bge-m3`).

### MS MARCO MiniLM & TinyBERT Cross-Encoders
- **Project:** [sentence-transformers / cross-encoder](https://www.sbert.net/docs/pretrained_cross-encoders.html)
- **License:** Apache License 2.0
- **Description:** Compact and highly accurate pre-trained cross-encoder models fine-tuned on MS MARCO passage retrieval.

### Jina AI Reranker
- **Project:** [jina-reranker-v2-base-multilingual](https://huggingface.co/jinaai/jina-reranker-v2-base-multilingual)
- **Author:** Jina AI
- **License:** Apache License 2.0
- **Description:** State-of-the-art multilingual and code-focused cross-encoder model supporting up to 8K token context windows.

---

## Core Libraries & Dependencies

### SQLite & FTS5 (mattn/go-sqlite3)
- **Project:** [github.com/mattn/go-sqlite3](https://github.com/mattn/go-sqlite3)
- **Author:** Yasuhiro Matsumoto (mattn)
- **License:** MIT License
- **Description:** SQLite3 driver for Go with full FTS5 full-text search support.

### Hierarchical Navigable Small World (coder/hnsw)
- **Project:** [github.com/coder/hnsw](https://github.com/coder/hnsw)
- **Author:** Coder Technologies Inc.
- **License:** MIT License
- **Description:** Pure Go implementation of the HNSW vector indexing algorithm.

### SIMD Vector Math (viterin/vek)
- **Project:** [github.com/viterin/vek](https://github.com/viterin/vek)
- **Author:** viterin
- **License:** Apache License 2.0
- **Description:** Hardware-accelerated SIMD vector arithmetic and cosine similarity in Go.

### PDF Rasterization (gen2brain/go-fitz)
- **Project:** [github.com/gen2brain/go-fitz](https://github.com/gen2brain/go-fitz)
- **Author:** gen2brain (MuPDF by Artifex Software)
- **License:** AGPL-3.0 / Commercial
- **Description:** Go bindings for MuPDF for rasterizing and rendering PDF documents.

### Zstandard Compression (klauspost/compress)
- **Project:** [github.com/klauspost/compress](https://github.com/klauspost/compress)
- **Author:** Klaus Post
- **License:** BSD 3-Clause License
- **Description:** Optimized Zstandard and general compression algorithms in Go.

### CLI Parser (alecthomas/kong)
- **Project:** [github.com/alecthomas/kong](https://github.com/alecthomas/kong)
- **Author:** Alec Thomas
- **License:** MIT License
- **Description:** Command-line parser for Go struct definitions.

### Stemming & Tokenization (kljensen/snowball)
- **Project:** [github.com/kljensen/snowball](https://github.com/kljensen/snowball)
- **Author:** Keith Jensen
- **License:** MIT License
- **Description:** Snowball stemmer port in Go for English and Turkish text analysis.

### Path & Pattern Matching (bmatcuk/doublestar)
- **Project:** [github.com/bmatcuk/doublestar](https://github.com/bmatcuk/doublestar)
- **Author:** Bob Matcuk
- **License:** MIT License
- **Description:** Glob and pattern matching with `**` multi-directory support in Go.
