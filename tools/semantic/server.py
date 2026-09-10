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
    # corpus_lang is the detected ISO 639-1 for the whole document
    # (contract v0.2.0); seek persists it as the ``language`` fast field.
    corpus_lang: Optional[str] = None


@app.get("/health")
def health():
    return {
        "status": "ok",
        "version": "0.2.0",
        "models": pipeline.model_status(),
    }


@app.post("/tag", response_model=TagResponse)
def tag(req: TagRequest) -> TagResponse:
    errors: list[TagError] = []
    items: list[dict] = []
    for ch in req.chunks:
        text = ch.text[:MAX_CHUNK_CHARS]
        if not text.strip():
            # Empty chunks are skipped, not errors.
            continue
        items.append({"id": ch.id, "text": text, "lang": ch.lang})

    # A single batch is one document (the Go client sends a document's
    # chunks together), so corpus-level operations (LID, BERTopic fit)
    # are meaningful and run once per request.
    if not items:
        return TagResponse(results=[], errors=errors, corpus_lang=None)

    try:
        results_raw, corpus_lang = pipeline.tag_batch(items, max_tags=req.max_tags)
    except Exception as e:  # pipeline-level failure degrades, never 500
        return TagResponse(
            results=[],
            errors=[TagError(id=-1, message=str(e))],
            corpus_lang=None,
        )

    out = TagResponse(results=[], errors=errors, corpus_lang=corpus_lang)
    for r in results_raw:
        out.results.append(
            TagResult(
                id=r["id"],
                tags=r["tags"],
                topics=[Topic(label=t["label"], score=t["score"]) for t in r["topics"]],
                entities=[Entity(text=e["text"], type=e["type"]) for e in r["entities"]],
            )
        )
    return out


if __name__ == "__main__":
    host = os.environ.get("SEMANTIC_HOST", "127.0.0.1")
    port = int(os.environ.get("SEMANTIC_PORT", "8003"))
    # SEMANTIC_WARMUP=1 eagerly loads the heavy models at startup so the
    # first /tag call (and therefore the first document indexed by seek)
    # already has topic + NER + LID available instead of cold-starting on
    # that very first request.
    if os.environ.get("SEMANTIC_WARMUP") == "1":
        pipeline._get_lid()
        pipeline._get_spacy("en")
        pipeline._bertopic_available()
        pipeline._get_yake()
    uvicorn.run(app, host=host, port=port)
