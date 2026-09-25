# Semantic Tag Service

The optional service at `scripts/services/semantic/` produces the `tags`, `topics`,
`entities`, and `language` fast fields. From a repository checkout, run
`uv run tools/semantic/server.py`; from an installed skill, run
`uv run "$SEMANTIC_DIR/server.py"`, where `SEMANTIC_DIR` points to the skill's
`scripts/services/semantic` directory. Both use deliberately degraded mode:
they install only the lightweight PEP 723 dependencies and normally expose
YAKE keyphrases only.

For the full offline pipeline (fastText LID, spaCy NER, YAKE, and BERTopic),
install and run through a virtual environment. When running from the installed
agent plugin, set `SEEK_SEMANTIC_HOME` to a writable user data directory so
setup does not write into the plugin cache. If unset, setup keeps the current
repository-local `.venv` behavior. Resolve the service directory from the
installed skill location; do not assume the user's current working directory.

```bash
# Linux/macOS
SKILL_DIR="/path/to/installed/seek-skill"
SEMANTIC_DIR="$SKILL_DIR/scripts/services/semantic"
export SEEK_SEMANTIC_HOME="${XDG_DATA_HOME:-$HOME/.local/share}/seek/semantic"
"$SEMANTIC_DIR/setup.sh"
SEMANTIC_WARMUP=1 "$SEEK_SEMANTIC_HOME/.venv/bin/python" "$SEMANTIC_DIR/server.py"
```

```powershell
# Windows PowerShell
$SkillDir = "C:\path\to\installed\seek-skill"
$SemanticDir = Join-Path $SkillDir "scripts\services\semantic"
$env:SEEK_SEMANTIC_HOME = Join-Path $env:LOCALAPPDATA "seek\semantic"
$SetupScript = Join-Path $SemanticDir "setup.ps1"
& $SetupScript
$env:SEMANTIC_WARMUP="1"
& (Join-Path $env:SEEK_SEMANTIC_HOME ".venv\Scripts\python.exe") (Join-Path $SemanticDir "server.py")
```

Never replace the final command with `uv run`: that selects the degraded
script environment instead of `.venv`.

Verify `GET http://127.0.0.1:8003/health` reports all four model flags as
`true`. Run a strict local check in the full environment with:

```bash
REQUIRE_FULL_SEMANTIC=1 "$SEEK_SEMANTIC_HOME/.venv/bin/python" \
  "$SEMANTIC_DIR/test_server.py"
```

```powershell
$env:REQUIRE_FULL_SEMANTIC="1"
& (Join-Path $env:SEEK_SEMANTIC_HOME ".venv\Scripts\python.exe") (Join-Path $SemanticDir "test_server.py")
```

After enabling the full service, refresh existing metadata with:

```bash
seek collection reindex <collection> --semantic-only
```
