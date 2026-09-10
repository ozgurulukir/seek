# /// script
# requires-python = ">=3.10"
# dependencies = [
#   "fastapi",
#   "uvicorn",
#   "yake",
# ]
# ///
"""seek semantic tag service.

A local NLP endpoint that enriches chunks with automatic metadata:
language detection (LID), named entities (NER), keyphrases (YAKE) and
topics (BERTopic). seek talks to this service through the stable JSON
envelope defined in the ADR (`.nova/plans/2026-09-10-semantic-tag-adr.md`);
internal model/runtime formats never leak into the contract.

Design notes (D1-D9):
  - Monorepo: this service lives in this repo under ``tools/semantic/``
    (mirroring ``tools/xberg_server/``). It is an OPTIONAL capability:
    seek works fully without it.
  - We do not host the model runtime by default. Run this file with
    ``uv run`` (PEP 723, light deps) for a degraded pipeline, or run
    ``setup.sh`` first to install the heavy models (spaCy / BERTopic /
    fasttext LID) into a local ``.venv`` for the full pipeline.
  - Local-first: binds to 127.0.0.1 only (override with SEMANTIC_HOST).

Run:
    uv run tools/semantic/server.py            # degraded (YAKE only)
    tools/semantic/setup.sh && uv run tools/semantic/server.py   # full

Endpoints:
    GET  /health   -> {"status": "ok", "models": {...}}
    POST /tag      -> {"results": [{"id", "tags", "topics", "entities"}], "errors": [...]}
"""

from __future__ import annotations

import os
from typing import Optional

import uvicorn
from fastapi import FastAPI
from pydantic import BaseModel, Field

from tagger import TagPipeline

app = FastAPI(title="seek semantic tag service")

pipeline = TagPipeline()

# Safety caps for the contract.
MAX_TAGS_HARD_LIMIT = 20
MAX_CHUNK_CHARS = 50_000


class TagChunk(BaseModel):
    id: int = Field(description="Seek chunk id (echoed back)")
    text: str = Field(description="Chunk text to tag")
    lang: Optional[str] = Field(
        default=None,
        description="ISO 639-1 hint (e.g. 'tr', 'en'). Omit to run language detection.",
    )


class TagRequest(BaseModel):
    chunks: list[TagChunk]
    max_tags: int = Field(default=5, ge=1, le=MAX_TAGS_HARD_LIMIT)


class Entity(BaseModel):
    text: str
    type: str


class Topic(BaseModel):
    label: str
    score: float


class TagResult(BaseModel):
    id: int
    tags: list[str]
    topics: list[Topic]
    entities: list[Entity]


class TagError(BaseModel):
    id: int
    message: str


class TagResponse(BaseModel):
    results: list[TagResult]
    errors: list[TagError]


@app.get("/health")
def health():
    return {
        "status": "ok",
        "version": "0.1.0",
        "models": pipeline.model_status(),
    }


@app.post("/tag", response_model=TagResponse)
def tag(req: TagRequest) -> TagResponse:
    results: list[TagResult] = []
    errors: list[TagError] = []
    for ch in req.chunks:
        text = ch.text[:MAX_CHUNK_CHARS]
        if not text.strip():
            continue  # empty chunks are skipped, not errors
        try:
            r = pipeline.tag(text, lang=ch.lang, max_tags=req.max_tags)
            results.append(
                TagResult(
                    id=ch.id,
                    tags=r["tags"],
                    topics=[Topic(label=t["label"], score=t["score"]) for t in r["topics"]],
                    entities=[Entity(text=e["text"], type=e["type"]) for e in r["entities"]],
                )
            )
        except Exception as e:  # per-chunk failure must not sink the batch
            errors.append(TagError(id=ch.id, message=str(e)))
    return TagResponse(results=results, errors=errors)


if __name__ == "__main__":
    host = os.environ.get("SEMANTIC_HOST", "127.0.0.1")
    port = int(os.environ.get("SEMANTIC_PORT", "8003"))
    uvicorn.run(app, host=host, port=port)
