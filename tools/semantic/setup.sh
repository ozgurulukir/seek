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
# After setup, use the venv interpreter directly (do not use `uv run`):
#   SEMANTIC_WARMUP=1 tools/semantic/.venv/bin/python tools/semantic/server.py
#
# Environment:
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

python bootstrap_models.py

echo
echo "Setup complete."
echo "Start the service with:"
echo "  SEMANTIC_WARMUP=1 tools/semantic/.venv/bin/python tools/semantic/server.py"
echo "Verify all capabilities with:"
echo "  REQUIRE_FULL_SEMANTIC=1 tools/semantic/.venv/bin/python tools/semantic/test_server.py"
