# Semantic tag service

The semantic tag service (`tools/semantic/`) is a local NLP endpoint that
enriches text chunks with automatic metadata: **tags** (keyphrases + topic
labels), **entities** (NER) and **topics** (topic modeling). It is an
**optional capability** — seek works fully without it; keyword search is
unaffected.

## What it does

For each chunk it returns a stable JSON envelope (contract-first: internal
model formats never leak into the contract):

```jsonc
// POST /tag
{
  "chunks": [
    {"id": 1, "text": "In Go, the concurrency model is goroutines...", "lang": "en"}
    // "lang" is optional — omit to run language detection
  ],
  "max_tags": 5
}
// ->
{
  "results": [
    {
      "id": 1,
      "tags": ["goroutines and channels", "concurrency model", "channels"],
      "topics": [{"label": "...", "score": 0.82}],
      "entities": [{"text": "OpenAI", "type": "MISC"}, {"text": "Go", "type": "LOC"}]
    }
  ],
  "corpus_lang": "en",   // detected ISO 639-1 for the whole document
  "errors": []           // pipeline-level failures surface here; a healthy
                         // request returns empty
}
```

| Facet | Component | Notes |
|---|---|---|
| `tags` | YAKE keyphrases (+ BERTopic labels when available) | merged, deduped, capped at `max_tags` |
| `entities` | spaCy NER (`xx_ent_wiki_sm`; `tr_core_news_sm` for Turkish when installed) | multilingual (50+ languages) |
| `topics` | BERTopic (multilingual embedding, PCA + min_samples) | each chunk carries its topic label; a theme needs only 2+ chunks to form one |
| language | `fasttext-langdetect` | reported once per request as `corpus_lang` |

## How seek consumes it

When `semantic.enabled: true` in `~/.config/seek/config.yaml`, seek sends each
document's chunks to `POST /tag` and stores four fast fields on the document
(pdf / documents / conversation collections only):

- `tags`, `topics`, `entities`, `language`

`tags` is a first-class filter (`--tag`); `topics`, `entities` and
`language` are facetable (no dedicated filter flag). All four facet with
`--aggs`:

```bash
seek search "query" --tag <t>                    # tags (frontmatter + semantic)
seek search "query" --aggs tags:terms --aggs topics:terms \
  --aggs entities:terms --aggs language:terms
```

Enrichment is always optional and degrades gracefully: if the service is down
or the capability is disabled, seek emits a WARN and proceeds without tags —
keyword search is unaffected. Under `privacy.offline_only` only numeric
loopback endpoints (`127.0.0.0/8`, `[::1]`) are accepted.

## Run it

The service is a monorepo component (like `tools/xberg_server/`). It binds to
`127.0.0.1:8003` by default. Two ways to run it:

**Degraded (no setup, keyphrases only):**
```bash
uv run tools/semantic/server.py
```

**Full pipeline (LID + NER + keyphrases + topics):**
```bash
tools/semantic/setup.sh                  # one-time: venv + models
source tools/semantic/.venv/bin/activate
SEMANTIC_WARMUP=1 python tools/semantic/server.py   # eager model load (recommended)
```
Run `setup.sh` once. Model weights are downloaded on first run, not
vendored into the repo (D9). `SEMANTIC_WARMUP=1` loads heavy models at
startup so the first indexed document already has topics/NER/LID; without
it they load lazily on the first request.

The Turkish spaCy model (`tr_core_news_sm`) is optional — only published
for spaCy 3.4–3.5; on newer spaCy the service falls back to the
multilingual `xx_ent_wiki_sm`.

Environment: `SEMANTIC_HOST` (default `127.0.0.1`), `SEMANTIC_PORT` (default `8003`).

## Endpoints

- `GET /health` → `{"status": "ok", "version": "...", "models": {"lid", "ner", "keyphrase", "topic"}}` — which capabilities are active.
- `POST /tag` → the envelope above.

## Design (ADR)

Full architecture rationale: `.nova/plans/2026-09-10-semantic-tag-adr.md`.
Key decisions: monorepo, contract-first, provider-agnostic (switch `backend` +
`base_url` without code changes), we do not host the model runtime (we ship
docs + setup scripts + permissively-licensed deps only, `vendor-LICENSES.md`).
