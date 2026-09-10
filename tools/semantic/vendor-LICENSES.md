# License notes — `tools/semantic`

D9: only permissive-licensed dependencies may be vendored. This file is
the license check for the semantic service.

| Dependency | License | Notes |
|---|---|---|
| fastapi | MIT | |
| uvicorn | BSD-3-Clause | |
| pydantic | MIT | |
| yake | MIT | |
| spacy | MIT | |
| spaCy models `xx_ent_wiki_sm` / `tr_core_news_sm` | MIT | downloaded by `setup.sh`, not vendored |
| fasttext-langdetect | MIT | wrapper; underlying fastText is MIT |
| fastText (transitive) | MIT | |
| bertopic | MIT | |
| sentence-transformers | Apache-2.0 | |
| `paraphrase-multilingual-MiniLM-L12-v2` weights | Apache-2.0 | downloaded at first run, not vendored |

Model weights are **not** vendored into the repo (D9): `setup.sh` and
first-run download pull them from Hugging Face / spaCy into the local
cache. Nothing copyleft (GPL/AGPL) is used or vendored here.
