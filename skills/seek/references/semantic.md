# Semantic Tag Service

The optional service at `tools/semantic/` produces the `tags`, `topics`,
`entities`, and `language` fast fields. `uv run tools/semantic/server.py` is
deliberately degraded mode: it installs only the lightweight PEP 723
dependencies and normally exposes YAKE keyphrases only.

For the full offline pipeline (fastText LID, spaCy NER, YAKE, and BERTopic),
install and run through the service's `.venv`:

```bash
# Linux/macOS
tools/semantic/setup.sh
SEMANTIC_WARMUP=1 tools/semantic/.venv/bin/python tools/semantic/server.py
```

```powershell
# Windows PowerShell
tools/semantic/setup.ps1
$env:SEMANTIC_WARMUP="1"
& tools/semantic/.venv/Scripts/python.exe tools/semantic/server.py
```

Never replace the final command with `uv run`: that selects the degraded
script environment instead of `.venv`.

Verify `GET http://127.0.0.1:8003/health` reports all four model flags as
`true`. Run a strict local check in the full environment with:

```bash
REQUIRE_FULL_SEMANTIC=1 tools/semantic/.venv/bin/python tools/semantic/test_server.py
```

```powershell
$env:REQUIRE_FULL_SEMANTIC="1"
& tools/semantic/.venv/Scripts/python.exe tools/semantic/test_server.py
```

After enabling the full service, refresh existing metadata with:

```bash
seek collection reindex <collection> --semantic-only
```
