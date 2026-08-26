# /// script
# dependencies = [
#   "fastembed>=0.4.0",
#   "fastapi>=0.110.0",
#   "uvicorn>=0.28.0",
#   "pydantic>=2.0.0",
# ]
# ///

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import List, Union, Optional
from fastembed import TextEmbedding
import uvicorn
import os
import argparse

# Default ultra-lightweight ONNX model from Seek docs: all-MiniLM-L6-v2 (~22MB, 384 dims, ~2-5ms on CPU)
DEFAULT_MODEL = os.environ.get("FASTEMBED_MODEL", "sentence-transformers/all-MiniLM-L6-v2")

DIMENSIONS_MAP = {
    "sentence-transformers/all-MiniLM-L6-v2": 384,
    "all-MiniLM-L6-v2": 384,
    "BAAI/bge-small-en-v1.5": 384,
    "bge-small-en-v1.5": 384,
    "nomic-ai/nomic-embed-text-v1.5": 768,
    "nomic-embed-text": 768,
    "BAAI/bge-base-en-v1.5": 768,
    "BAAI/bge-large-en-v1.5": 1024,
}

print(f"Loading FastEmbed ONNX model: {DEFAULT_MODEL}...")
embedding_model = TextEmbedding(model_name=DEFAULT_MODEL)
model_dim = DIMENSIONS_MAP.get(DEFAULT_MODEL, 384)
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
    port = int(os.environ.get("PORT", "8002"))
    print(f"Starting FastEmbed OpenAI-compatible server on http://127.0.0.1:{port}")
    uvicorn.run(app, host="127.0.0.1", port=port)
