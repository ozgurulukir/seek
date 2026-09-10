#!/usr/bin/env bash
# Setup for the seek semantic tag service (tools/semantic).
#
# Installs the heavy NLP stack into a local .venv so the full pipeline
# (LID + NER + keyphrase + topics) is available. The service also boots
# WITHOUT this setup (`uv run server.py`) in degraded mode (keyphrases
# only); this script just adds the optional models.
#
# Usage:
#   tools/semantic/setup.sh
#
# After setup:
#   source tools/semantic/.venv/bin/activate
#   python tools/semantic/server.py
#
Source env:
#   SEMANTIC_HOST (default 127.0.0.1)
#   SEMANTIC_PORT (default 8003)
#   SEMANTIC_WARMUP=1  — eagerly load heavy models (spaCy/BERTopic/LID) at
#                        startup so the first request is fast (recommended
#                        for seek integration).
#
# Note: heavy model weights (spaCy xx_ent_wiki_sm, tr_core_news_sm,
# paraphrase-multilingual-MiniLM-L12-v2, LID vectors) are downloaded on
# first run, not by this script.
set -euo pipefail

cd "$(dirname "$0")"

if ! command -v uv >/dev/null 2>&1; then
  echo "error: uv not found in PATH (https://docs.astral.sh/uv/)" >&2
  exit 1
fi

if [ -d .venv ]; then
  echo "==> venv already exists (tools/semantic/.venv) — reusing"
else
  echo "==> Creating venv (tools/semantic/.venv)"
  uv venv .venv --python 3.11
fi

echo "==> Installing heavy NLP stack"
# shellcheck disable=SC1091
source .venv/bin/activate
uv pip install -r requirements.txt

echo "==> Downloading spaCy models (xx_ent_wiki_sm, tr_core_news_sm [optional])"
python - <<'PY'
import spacy

def get(model):
    try:
        spacy.load(model)
        print(f"    {model}: already present")
        return True
    except OSError:
        pass
    try:
        # spacy.cli.download raises SystemExit on failure (Typer),
        # which is not an Exception — catch BaseException here.
        spacy.cli.download(model)
        spacy.load(model)
        print(f"    {model}: installed")
        return True
    except BaseException as e:
        print(f"    {model}: SKIP ({type(e).__name__})")
        return False

get("xx_ent_wiki_sm")
# Optional: official Turkish model is only published for spaCy 3.4-3.5;
# on newer spaCy this fails and the service falls back to xx_ent_wiki_sm.
get("tr_core_news_sm")
PY

echo "==> Downloading LID model (fasttext-langdetect vectors)"
python - <<'PY'
try:
    import ftlangdetect  # fasttext-langdetect >= 1.0
    r = ftlangdetect.detect("test")
    print("    lid vectors: ready (v1 API)")
except ImportError:
    from fasttext_langdetect import LangDetector  # < 1.0
    LangDetector().detect("test")
    print("    lid vectors: ready (legacy API)")
PY

echo
echo "Setup complete."
echo "Start the service with:"
echo "  source tools/semantic/.venv/bin/activate"
echo "  SEMANTIC_WARMUP=1 python tools/semantic/server.py   # prefer: eager model load"
echo "  python tools/semantic/server.py                       # lazy first-request load"
