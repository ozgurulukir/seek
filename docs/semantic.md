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

Semantic enrichment is two-tiered. Conversations (claude/codex), PDF, and
documents are enriched during the regular `seek sync` pass. Markdown, code,
and parser collections are not enriched during sync; they are brought to
parity by the semantic backfill (`seek collection reindex <name>
--semantic-only`), which re-enriches every document whose fingerprint is
stale or absent. When `semantic.enabled: true` in
`~/.config/seek/config.yaml` and the service is healthy, seek sends each
document's chunks to `POST /tag` and stores four fast fields on the
document:

- `tags`, `topics`, `entities`, `language`

All four are filterable with the generic fast-field flag `--field
<name>:<value>`, and facetable with `--aggs`:

```bash
seek search "query" --field tags:go                      # comma-list membership
seek search "query" --field "topics:ownership and compile free"
seek search "query" --field "entities:ORG:OpenAI"
seek search "query" --field language:en                  # exact (single value)

seek search "query" --aggs tags:terms --aggs topics:terms \
  --aggs entities:terms --aggs language:terms
```

Match semantics are per field type: `tags`/`topics`/`entities` are
comma-list membership (match a whole comma-separated token), while
`language` and the code fields (`lang`, `repo`, ...) match exactly. The old
`--tag` flag was removed; `--field tags:<value>` gives identical behaviour.

### Service-down behavior

Enrichment is always optional and degrades gracefully. If the service is down,
times out, returns malformed data, or lacks a capability, seek still succeeds:

- the sync or backfill pass completes — failures surface as `WARN` lines, never
  as a hard error, and keyword (BM25) search is unaffected;
- previously stored semantic fast fields are preserved — a failed enrichment
  never wipes existing `tags`/`topics`/`entities`/`language` values;
- during **backfill**, an enrichment failure records status `error` for the
  document (its prior fields are kept) and the next backfill pass retries it;
- during **sync**, a document re-enriched while the service is down records
  status `stale` (prior fields kept) so the next backfill re-enriches it; a
  document whose enrichment was never attempted stays `none`. The service-down
  `stale`/`error` markers are written by the precise pass that failed — sync
  and backfill each record their own outcome.

Under `privacy.offline_only` only numeric loopback endpoints (`127.0.0.0/8`,
`[::1]`) are accepted.

### Semantic fingerprinting and status

Every document stores a `semantic_fingerprint` and a `semantic_status`
(`current`, `stale`, or `error`), computed from:

- the semantic service identity (base URL),
- the capability set the service reports via `/health` (LID, NER,
  keyphrases, topics),
- the enrichment schema version (`v1`), and
- a per-document hash of its indexed chunk content.

Sync and backfill compute the same fingerprint. **Sync** records it on every
document it enriches (conversations, PDFs, documents): a successful enrichment
marks the document `current`; a re-sync while the service is down marks it
`stale` (prior fields preserved). **Backfill** — and the backfill alone —
_consults_ the stored fingerprint: a document whose fingerprint matches the
current desired identity is `current` and the backfill skips it (no re-call),
while a change in the service identity, capabilities, schema version, or
document content leaves the document `stale`, so the next backfill re-enriches
it. A document with no recorded state is reported as `none` by
`seek collection list/show` (e.g. semantic enrichment disabled, or a type never
synced with the service up); it becomes `current`/`stale` on the next sync or
backfill that actually attempts enrichment.

### Backfill (`seek collection reindex <name> --semantic-only`)

The semantic backfill is the tool for bringing a collection up to date when
documents are `stale` or unenriched (e.g. the service was down during
indexing). It:

- selects every document whose fingerprint is stale or absent,
- re-enriches it **from its already-indexed chunks**,
- atomically updates the fast fields and the fingerprint/status.

It never re-reads source files, and never touches the FTS index, embeddings,
or the vector index — only the semantic fast fields and fingerprint/status
change. Progress prints `N processed, M skipped, K failed`; documents that
fail keep their prior fields and get status `error`, so the next pass retries
them.

A normal `seek collection reindex <name>` (without `--semantic-only`) instead
re-reads the source files and rebuilds chunks, FTS, fast fields, and
embeddings for the collection.

### Source files are never modified

No `seek` command — `add`, `sync` (incl. `sync <collection> --path`), `embed`,
`collection rename`/`reindex`/`--semantic-only` backfill, `status`, or `rm` —
ever modifies, deletes, or renames source files. All management commands only
read your files and manage the local index (SQLite database, FTS, fast fields,
vector index). Your files remain the source of truth.

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
