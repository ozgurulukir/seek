# /// script
# requires-python = ">=3.10"
# dependencies = [
#   "fastapi",
#   "pyyaml",
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
import ipaddress
from typing import Optional
from collections.abc import Mapping

import uvicorn
from fastapi import FastAPI
from pydantic import BaseModel, Field

from tagger import TagPipeline

app = FastAPI(title="seek semantic tag service")

def load_config() -> dict:
    """Load seek's semantic service settings from the user config."""
    config_path = os.environ.get(
        "SEEK_CONFIG", os.path.expanduser("~/.config/seek/config.yaml")
    )
    try:
        import yaml
        with open(config_path, encoding="utf-8") as handle:
            config = yaml.safe_load(handle) or {}
            if isinstance(config, Mapping):
                return dict(config)
            print(f"warning: ignoring non-mapping config in {config_path}")
            return {}
    except Exception as exc:
        print(f"warning: unable to read {config_path}: {exc}")
        return {}


CONFIG = load_config()
SEMANTIC_CONFIG = CONFIG.get("semantic")
if not isinstance(SEMANTIC_CONFIG, Mapping):
    SEMANTIC_CONFIG = {}
PRIVACY_CONFIG = CONFIG.get("privacy")
if not isinstance(PRIVACY_CONFIG, Mapping):
    PRIVACY_CONFIG = {}


def as_bool(value: object, default: bool = False) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        return value.strip().lower() in {"1", "true", "yes", "on"}
    return default


OFFLINE_ONLY = as_bool(PRIVACY_CONFIG.get("offline_only", False))


def configured_host(value: object) -> str:
    """Apply loopback enforcement only when offline_only is enabled."""
    if not OFFLINE_ONLY:
        return str(value or "127.0.0.1")
    return loopback_host(value)


def loopback_host(value: object) -> str:
    """Keep the local helper bound to a loopback interface."""
    candidate = str(value or "127.0.0.1")
    try:
        if ipaddress.ip_address(candidate).is_loopback:
            return candidate
    except ValueError:
        pass
    print(f"warning: refusing non-loopback semantic host {candidate!r}")
    return "127.0.0.1"


SEMANTIC_ENABLED = as_bool(SEMANTIC_CONFIG.get("enabled", False))
try:
    DEFAULT_MAX_TAGS = min(max(int(SEMANTIC_CONFIG.get("max_tags", 5)), 1), 20)
except (TypeError, ValueError):
    DEFAULT_MAX_TAGS = 5
SERVICE_HOST = os.environ.get(
    "SEMANTIC_HOST",
    SEMANTIC_CONFIG.get("host") or "127.0.0.1",
)
SERVICE_HOST = configured_host(SERVICE_HOST)
try:
    SERVICE_PORT = int(os.environ.get(
        "SEMANTIC_PORT",
        SEMANTIC_CONFIG.get("port") or 8003,
    ))
    if SERVICE_PORT <= 0 or SERVICE_PORT > 65535:
        raise ValueError
except (TypeError, ValueError):
    SERVICE_PORT = 8003

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
    max_tags: int = Field(default=DEFAULT_MAX_TAGS, ge=1, le=MAX_TAGS_HARD_LIMIT)


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
    if not SEMANTIC_ENABLED:
        print("warning: semantic.enabled is false; serving anyway for diagnostics")
    # SEMANTIC_WARMUP=1 eagerly loads the heavy models at startup so the
    # first /tag call (and therefore the first document indexed by seek)
    # already has topic + NER + LID available instead of cold-starting on
    # that very first request.
    if os.environ.get("SEMANTIC_WARMUP") == "1":
        pipeline._get_lid()
        pipeline._get_spacy("en")
        pipeline._bertopic_available()
        pipeline._get_yake()
    uvicorn.run(app, host=SERVICE_HOST, port=SERVICE_PORT)
