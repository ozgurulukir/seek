# /// script
# dependencies = [
#   "flashrank",
#   "fastapi",
#   "pyyaml",
#   "uvicorn",
# ]
# ///

import os
import ipaddress
from pathlib import Path
from typing import Any
from collections.abc import Mapping

from fastapi import FastAPI
from pydantic import BaseModel
from typing import List, Optional
from flashrank import Ranker, RerankRequest
import uvicorn

app = FastAPI(title="Local FlashRank Reranker")

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
            return {}
    except Exception as exc:
        print(f"warning: unable to read {config_path}: {exc}")
        return {}


CONFIG = load_config()
RERANK_CONFIG = CONFIG.get("rerank")
if not isinstance(RERANK_CONFIG, Mapping):
    RERANK_CONFIG = {}
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


def configured_host(value: Any) -> str:
    """Apply loopback enforcement only when offline_only is enabled."""
    if not OFFLINE_ONLY:
        return str(value or "127.0.0.1")
    return loopback_host(value)


def loopback_host(value: Any) -> str:
    """Keep the local helper bound to a loopback interface."""
    candidate = str(value or "127.0.0.1")
    try:
        if ipaddress.ip_address(candidate).is_loopback:
            return candidate
    except ValueError:
        pass
    print(f"warning: refusing non-loopback reranker host {candidate!r}")
    return "127.0.0.1"


# Multilingual model (~150MB) with Turkish coverage. Environment variables
# override config for container/service-manager deployments.
MODEL_NAME = os.getenv("FLASHRANK_MODEL") or RERANK_CONFIG.get(
    "model", "ms-marco-MultiBERT-L-12"
)
if not isinstance(MODEL_NAME, str) or not MODEL_NAME.strip():
    MODEL_NAME = "ms-marco-MultiBERT-L-12"
HOST = os.getenv(
    "FLASHRANK_HOST",
    RERANK_CONFIG.get("host") or "127.0.0.1",
)
HOST = configured_host(HOST)


def positive_int(value: Any, default: int) -> int:
    try:
        parsed = int(value)
        return parsed if parsed > 0 else default
    except (TypeError, ValueError):
        return default


PORT = positive_int(
    os.getenv("FLASHRANK_PORT", RERANK_CONFIG.get("port") or 8000),
    8000,
)
if PORT > 65535:
    PORT = 8000
DEFAULT_TOP_N = positive_int(RERANK_CONFIG.get("top_n", 10), 10)
ranker = Ranker(model_name=MODEL_NAME)

class RerankPayload(BaseModel):
    model: Optional[str] = None
    query: str
    documents: List[str]
    top_n: Optional[int] = None

@app.post("/rerank")
@app.post("/v1/rerank")
def handle_rerank(req: RerankPayload):
    passages = [{"id": idx, "text": doc} for idx, doc in enumerate(req.documents)]
    rerank_req = RerankRequest(query=req.query, passages=passages)
    results = ranker.rerank(rerank_req)
    
    top_n = req.top_n if req.top_n and req.top_n > 0 else DEFAULT_TOP_N
    top_n = min(top_n, len(results))
    results = results[:top_n]
    
    return {
        "results": [
            {"index": int(r["id"]), "relevance_score": float(r["score"])}
            for r in results
        ]
    }

@app.get("/health")
def health():
    return {"status": "ok", "model": MODEL_NAME}

if __name__ == "__main__":
    uvicorn.run(app, host=HOST, port=PORT)
