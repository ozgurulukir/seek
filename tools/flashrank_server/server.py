# /// script
# dependencies = [
#   "flashrank",
#   "fastapi",
#   "uvicorn",
# ]
# ///

from fastapi import FastAPI
from pydantic import BaseModel
from typing import List, Optional
from flashrank import Ranker, RerankRequest
import uvicorn

app = FastAPI(title="Local FlashRank Reranker")

# Default lightweight model ms-marco-TinyBERT-L-2-v2 (~4MB)
ranker = Ranker(model_name="ms-marco-TinyBERT-L-2-v2")

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
    
    top_n = req.top_n if req.top_n and req.top_n > 0 else len(results)
    results = results[:top_n]
    
    return {
        "results": [
            {"index": int(r["id"]), "relevance_score": float(r["score"])}
            for r in results
        ]
    }

@app.get("/health")
def health():
    return {"status": "ok", "model": "ms-marco-TinyBERT-L-2-v2"}

if __name__ == "__main__":
    uvicorn.run(app, host="127.0.0.1", port=8000)
