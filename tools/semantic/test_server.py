"""Contract tests for the seek semantic tag service.

Run (server must not be running — TestClient is in-process):
    uv run --with pytest tools/semantic/test_server.py

Or against a live server (degraded or full):
    SERVER_URL=http://127.0.0.1:8003 uv run --with requests tools/semantic/test_server.py
"""

from __future__ import annotations

import os

from tagger import TagPipeline

try:
    from fastapi.testclient import TestClient

    from server import app

    client = TestClient(app)
    MODE = "in-process"
except ImportError:
    import requests

    MODE = f"live @ {os.environ.get('SERVER_URL', 'http://127.0.0.1:8003')}"
    BASE = os.environ.get("SERVER_URL", "http://127.0.0.1:8003")

    def get(path):
        r = requests.get(BASE + path, timeout=30)
        r.raise_for_status()
        return r.json()

    def post(path, payload):
        r = requests.post(BASE + path, json=payload, timeout=60)
        r.raise_for_status()
        return r.json()


passed = 0


def check(name: str, cond: bool, detail: str = ""):
    global passed
    status = "PASS" if cond else "FAIL"
    print(f"  [{status}] {name}" + (f"  ({detail})" if detail and not cond else ""))
    if cond:
        passed += 1
    else:
        raise SystemExit(f"test failed: {name} {detail}")


def _health():
    if MODE == "in-process":
        r = client.get("/health")
        assert r.status_code == 200
        return r.json()
    return get("/health")


def _tag(payload):
    if MODE == "in-process":
        r = client.post("/tag", json=payload)
        assert r.status_code == 200
        return r.json()
    return post("/tag", payload)


def test_health():
    body = _health()
    check("health: status ok", body.get("status") == "ok")
    check("health: version present", "version" in body)
    check("health: models has 4 capabilities",
          set(body["models"]) == {"lid", "ner", "keyphrase", "topic"})


def test_tag_contract_shape():
    body = _tag({"chunks": [{"id": 1, "text": "OpenAI released GPT-4 in 2023."}], "max_tags": 3})
    check("tag: results is list", isinstance(body["results"], list))
    check("tag: errors is list", isinstance(body["errors"], list))
    res = body["results"][0]
    check("tag: id echoed", res["id"] == 1)
    check("tag: tags is list[str]", isinstance(res["tags"], list)
          and all(isinstance(t, str) for t in res["tags"]))
    check("tag: topics is list[label/score]",
          all(set(t) == {"label", "score"} for t in res["topics"]))
    check("tag: entities is list[text/type]",
          all(set(e) == {"text", "type"} for e in res["entities"]))
    check("tag: max_tags honored", len(res["tags"]) <= 3)


def test_tag_empty_chunk_skipped():
    body = _tag({"chunks": [{"id": 10, "text": "   "}], "max_tags": 3})
    check("empty chunk: not in results", all(r["id"] != 10 for r in body["results"]))
    check("empty chunk: not an error", body["errors"] == [])


def test_tag_batch_isolated():
    body = _tag({"chunks": [
        {"id": 1, "text": "Rust ownership and borrowing in systems programming."},
        {"id": 2, "text": "Go concurrency with goroutines and channels."},
    ], "max_tags": 3})
    ids = [r["id"] for r in body["results"]]
    check("batch: both chunks processed", ids == [1, 2], f"got {ids}")


def test_tag_max_tags_clamped():
    body = _tag({"chunks": [{"id": 1, "text": "The quick brown fox jumps over the lazy dog repeatedly."}],
                "max_tags": 20})
    check("max_tags: hard limit 20 honored", len(body["results"][0]["tags"]) <= 20)


def test_pipeline_direct():
    p = TagPipeline()
    r = p.tag("OpenAI released the GPT-4 model in 2023. Concurrency in Go.", max_tags=5)
    check("pipeline: keys complete", set(r) == {"tags", "topics", "entities"})
    check("pipeline: dedup is case-insensitive",
          len(r["tags"]) == len({t.lower() for t in r["tags"]}))


if __name__ == "__main__":
    print(f"semantic tag service tests ({MODE})")
    test_health()
    test_tag_contract_shape()
    test_tag_empty_chunk_skipped()
    test_tag_batch_isolated()
    test_tag_max_tags_clamped()
    test_pipeline_direct()
    print(f"\nall {passed} checks passed")
