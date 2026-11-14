# /// script
# dependencies = [
#   "fastembed>=0.4.0",
#   "fastapi>=0.110.0",
#   "pyyaml>=6.0",
#   "uvicorn>=0.28.0",
#   "pydantic>=2.0.0",
# ]
# ///

import ipaddress
import os
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import List, Union, Optional
from fastembed import TextEmbedding
import uvicorn

def load_config() -> dict[str, Any]:
    """Load seek's user config; environment variables remain overrides."""
    config_path = Path(os.environ.get(
        "SEEK_CONFIG", Path.home() / ".config" / "seek" / "config.yaml"
    ))
    try:
        import yaml
        with config_path.open(encoding="utf-8") as handle:
            config = yaml.safe_load(handle) or {}
            if isinstance(config, Mapping):
                return dict(config)
            print(f"warning: ignoring non-mapping config in {config_path}")
    except Exception as exc:
        print(f"warning: unable to read {config_path}: {exc}")
    return {}


CONFIG = load_config()
EMBEDDING_CONFIG = CONFIG.get("embedding")
if not isinstance(EMBEDDING_CONFIG, Mapping):
    EMBEDDING_CONFIG = {}
PRIVACY_CONFIG = CONFIG.get("privacy")
if not isinstance(PRIVACY_CONFIG, Mapping):
    PRIVACY_CONFIG = {}


def as_bool(value: Any, default: bool = False) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        return value.strip().lower() in {"1", "true", "yes", "on"}
    return default


OFFLINE_ONLY = as_bool(PRIVACY_CONFIG.get("offline_only", False))


def loopback_host(value: Any) -> str:
    candidate = str(value or "127.0.0.1")
    try:
        if ipaddress.ip_address(candidate).is_loopback:
            return candidate
    except ValueError:
        pass
    print(f"warning: refusing non-loopback embedding host {candidate!r}")
    return "127.0.0.1"


MODEL = os.environ.get("FASTEMBED_MODEL") or EMBEDDING_CONFIG.get(
    "model", "sentence-transformers/all-MiniLM-L6-v2"
)
if not isinstance(MODEL, str) or not MODEL.strip():
    MODEL = "sentence-transformers/all-MiniLM-L6-v2"
DEFAULT_MODEL = MODEL

DIMENSIONS_MAP = {
    "sentence-transformers/all-MiniLM-L6-v2": 384,
    "all-MiniLM-L6-v2": 384,
    "BAAI/bge-small-en-v1.5": 384,
    "bge-small-en-v1.5": 384,
    "nomic-ai/nomic-embed-text-v1.5": 768,
    "nomic-embed-text": 768,
    "BAAI/bge-base-en-v1.5": 768,
    "BAAI/bge-large-en-v1.5": 1024,
    "intfloat/multilingual-e5-small": 384,
}

try:
    model_dim = int(EMBEDDING_CONFIG.get("dimensions") or DIMENSIONS_MAP.get(DEFAULT_MODEL, 384))
    if model_dim <= 0:
        raise ValueError
except (TypeError, ValueError):
    model_dim = DIMENSIONS_MAP.get(DEFAULT_MODEL, 384)

HOST = os.environ.get(
    "EMBED_SERVER_HOST",
    EMBEDDING_CONFIG.get("host") or "127.0.0.1",
)
if OFFLINE_ONLY:
    HOST = loopback_host(HOST)
else:
    HOST = str(HOST or "127.0.0.1")
try:
    PORT = int(os.environ.get(
        "EMBED_SERVER_PORT",
        os.environ.get("PORT") or
        EMBEDDING_CONFIG.get("port") or 8002,
    ))
    if PORT <= 0 or PORT > 65535:
        raise ValueError
except (TypeError, ValueError):
    PORT = 8002

print(f"Loading FastEmbed ONNX model: {DEFAULT_MODEL}...")
embedding_model = TextEmbedding(model_name=DEFAULT_MODEL)
print(f"Model loaded successfully (dimensions: {model_dim}).")

app = FastAPI(title="Local FastEmbed OpenAI-Compatible Server")

class EmbeddingRequest(BaseModel):
    input: Union[str, List[str]]
    model: Optional[str] = None
    dimensions: Optional[int] = None

@app.get("/health")
def health():
    return {
        "status": "ok",
        "model": DEFAULT_MODEL,
        "dimensions": model_dim
    }

@app.get("/v1/models")
def list_models():
    return {
        "object": "list",
        "data": [
            {"id": DEFAULT_MODEL, "object": "model", "owned_by": "fastembed"}
        ]
    }

@app.post("/v1/embeddings")
@app.post("/embeddings")
def create_embeddings(req: EmbeddingRequest):
    if isinstance(req.input, str):
        texts = [req.input]
    elif isinstance(req.input, list):
        texts = req.input
    else:
        raise HTTPException(status_code=400, detail="Invalid input format")

    if not texts:
        return {"object": "list", "data": [], "model": DEFAULT_MODEL, "usage": {"prompt_tokens": 0, "total_tokens": 0}}

    try:
        # FastEmbed returns a generator of numpy float arrays
        embeddings_gen = embedding_model.embed(texts)
        embeddings_list = [emb.tolist() for emb in embeddings_gen]
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Embedding error: {str(e)}")

    data = [
        {
            "object": "embedding",
            "index": idx,
            "embedding": emb
        }
        for idx, emb in enumerate(embeddings_list)
    ]

    total_tokens = sum(len(t.split()) for t in texts)
    return {
        "object": "list",
        "data": data,
        "model": req.model or DEFAULT_MODEL,
        "usage": {
            "prompt_tokens": total_tokens,
            "total_tokens": total_tokens
        }
    }

if __name__ == "__main__":
    print(f"Starting FastEmbed OpenAI-compatible server on http://{HOST}:{PORT}")
    uvicorn.run(app, host=HOST, port=PORT)
