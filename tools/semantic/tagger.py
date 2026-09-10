"""Tag pipeline for the seek semantic tag service.

Batch API: one /tag request is one document (its chunks). Components:

  - LID        fasttext-langdetect  (corpus-level; reported once per request)
  - NER        spaCy                (per chunk)
  - Keyphrase  YAKE                 (per chunk)
  - Topics     BERTopic             (fit over the request's chunks, then
                                     transformed — a corpus-level algorithm)

All optional heavy dependencies are imported lazily inside try/except so
that the service boots and answers with whatever is installed. Model state
is reported via ``model_status()`` so ``/health`` (and therefore seek) can
see which capabilities are active. No model output format is ever passed
through raw: everything is normalized to the contract envelope before it
reaches FastAPI.

Contract v0.2.0 (2026-09-10):
  request  {"chunks": [{"id", "text", "lang?"}], "max_tags": n}
  response {"results": [{"id", "tags", "topics", "entities"}],
            "lang": "en", "errors": [...]}
``lang`` is detected corpus-level (ISO 639-1) when no chunk carries a hint;
the Go side persists it as the ``language`` fast field.
"""

from __future__ import annotations

import logging
import threading

log = logging.getLogger("semantic")

# BERTopic tuning: small batches (one document's chunks) don't need the
# default 50-dim UMAP; 10 dims keeps fit fast and stable.
BERTOPIC_MIN_CHUNKS = 3
BERTOPIC_UMAP_DIMS = 10
MAX_TOPIC_LABEL_WORDS = 4
MAX_ENTITIES_PER_CHUNK = 10


def _topic_label(model, topic_id: int) -> str:
    """Turn a BERTopic c-TF-IDF topic into a short readable label."""
    try:
        pairs = model.get_topic(topic_id)
    except Exception:
        return ""
    if not pairs:
        return ""
    words = [str(w) for w, _score in pairs[:MAX_TOPIC_LABEL_WORDS]]
    label = " ".join(words).strip()
    return label[:80]


class TagPipeline:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._lid = None
        self._lid_failed = False
        self._spacy_nlp: dict | None = None
        self._spacy_failed = False
        self._bertopic_factory = None
        self._bertopic_failed = False
        self._yake = None
        self._yake_failed = False
        # YAKE is a light hard dependency of the script: warm it at startup
        # so /health reports the real capability without a first /tag call.
        # Heavy models (spaCy, BERTopic) stay lazy.
        self._get_yake()

    # ------------------------------------------------------------------
    # capability status (exposed via /health)
    # ------------------------------------------------------------------

    def model_status(self) -> dict:
        return {
            "lid": self._lid is not None,
            "ner": bool(self._spacy_nlp),
            "keyphrase": self._yake is not None,
            "topic": self._bertopic_factory is not None,
        }

    # ------------------------------------------------------------------
    # lazy singletons
    # ------------------------------------------------------------------

    def _get_lid(self):
        if self._lid is None and not self._lid_failed:
            try:
                try:
                    # fasttext-langdetect >= 1.0
                    import ftlangdetect

                    self._lid = ("ftlangdetect", ftlangdetect)
                except ImportError:
                    # fasttext-langdetect < 1.0
                    from fasttext_langdetect import LangDetector

                    self._lid = ("legacy", LangDetector())
            except Exception as e:
                self._lid_failed = True
                log.warning("LID unavailable (%s); lang detection disabled", e)
        return self._lid

    def _get_spacy(self, lang: str = ""):
        # Turkish prefers tr_core_news_sm when installed; otherwise falls
        # back to the multilingual xx_ent_wiki_sm (the official Turkish
        # model is only published for spaCy 3.4-3.5, not 3.8). Cache per
        # loaded model name.
        if self._spacy_failed:
            return None
        if self._spacy_nlp is None:
            self._spacy_nlp = {}
        key = "tr_core_news_sm" if lang == "tr" else "xx_ent_wiki_sm"
        if key in self._spacy_nlp:
            return self._spacy_nlp[key]
        disabled = ["tok2vec", "tagger", "parser", "attribute_ruler"]
        try:
            import spacy

            try:
                nlp = spacy.load(key, disable=disabled)
            except OSError:
                if key != "xx_ent_wiki_sm":
                    log.warning("%s missing; falling back to xx_ent_wiki_sm", key)
                    key = "xx_ent_wiki_sm"
                if key in self._spacy_nlp:
                    return self._spacy_nlp[key]
                nlp = spacy.load(key, disable=disabled)
            self._spacy_nlp[key] = nlp
            return nlp
        except Exception as e:
            self._spacy_failed = True
            log.warning("spaCy NER unavailable (%s); entities disabled", e)
            return None

    def _get_yake(self):
        if self._yake is None and not self._yake_failed:
            try:
                try:
                    from yake import KeywordExtractor  # yake >= 0.7
                except ImportError:
                    from yake import KWExtractor as KeywordExtractor  # yake < 0.7

                self._yake = KeywordExtractor(n=3, top=8)
            except Exception as e:
                self._yake_failed = True
                log.warning("YAKE unavailable (%s); keyphrases disabled", e)
        return self._yake

    def _bertopic_available(self) -> bool:
        """Check (once) that BERTopic deps can be imported."""
        if self._bertopic_factory is None and not self._bertopic_failed:
            try:
                from sentence_transformers import SentenceTransformer

                from bertopic import BERTopic

                def factory():
                    # PCA (not UMAP) for dimensionality reduction: UMAP fails
                    # on small batches (n_neighbors > n_samples). PCA is fast,
                    # deterministic, and works for both small and large N.
                    from sklearn.decomposition import PCA

                    def make_model(n_docs: int):
                        comps = min(5, max(2, n_docs - 1))
                        return BERTopic(
                            embedding_model=SentenceTransformer(
                                "paraphrase-multilingual-MiniLM-L12-v2"
                            ),
                            umap_model=PCA(n_components=comps, random_state=42),
                        )

                    return make_model

                self._bertopic_factory = factory
            except Exception as e:
                self._bertopic_failed = True
                log.warning("BERTopic unavailable (%s); topics disabled", e)
        return self._bertopic_factory is not None

    # ------------------------------------------------------------------
    # per-component taggers
    # ------------------------------------------------------------------

    def _detect_lang(self, text: str) -> str:
        lid = self._get_lid()
        if lid is None:
            return "unknown"
        try:
            if lid[0] == "ftlangdetect":
                res = lid[1].detect(text)
                return str(res["lang"]).lower()
            lang, _score = lid[1].detect(text)
            return lang.lower()
        except Exception:
            return "unknown"

    def _ner(self, text: str, lang: str) -> list[dict]:
        nlp = self._get_spacy(lang)
        if nlp is None:
            return []
        try:
            doc = nlp(text)
        except Exception:
            return []
        ents: list[dict] = []
        for ent in doc.ents:
            if len(ent.text.strip()) < 2:
                continue
            ents.append({"text": ent.text.strip(), "type": ent.label_})
            if len(ents) >= MAX_ENTITIES_PER_CHUNK:
                break
        return ents

    def _keyphrases(self, text: str) -> list[str]:
        yake = self._get_yake()
        if yake is None:
            return []
        try:
            kws = yake.extract_keywords(text)
        except Exception:
            return []
        seen: dict[str, None] = {}
        for kw, _score in kws:
            k = kw.strip().lower()
            if 2 < len(k) <= 40:
                seen.setdefault(k, None)
        return list(seen)

    def _fit_topics(self, texts: list[str]) -> list[list[dict]]:
        """Fit BERTopic over the batch and return per-chunk top topics.

        Returns a list parallel to ``texts``; each entry is a list of
        {"label", "score"} dicts (usually 0-1 entries per chunk because
        document chunk sets are small).

        Two parameters matter for small N (3-8 chunks): HDBSCAN's default
        min_samples == min_cluster_size is too conservative and marks every
        point as noise, so we force min_samples=1 to let a >=2-chunk theme
        survive. A chunk whose theme has no second member stays noise (-1)
        and correctly gets no topic.
        """
        n = len(texts)
        if not self._bertopic_available() or n < BERTOPIC_MIN_CHUNKS:
            return [[] for _ in range(n)]
        with self._lock:
            try:
                model = self._bertopic_factory()(n)
                model.hdbscan_model.min_cluster_size = 2
                # Default min_samples == min_cluster_size is conservative
                # enough to noise out every point on small batches; lower it
                # so a >=2-chunk theme still forms a topic (HDBSCAN docs).
                model.hdbscan_model.min_samples = 1
                model.fit_transform(texts)
                # fit_transform/transform return parallel 1-D lists: per-chunk
                # (topic_id, probability). topic < 0 = HDBSCAN outlier (noise).
                topics, probs = model.transform(texts)
                out: list[list[dict]] = []
                for i in range(n):
                    topic_id, prob = topics[i], probs[i]
                    if topic_id < 0:
                        out.append([])
                        continue
                    label = _topic_label(model, int(topic_id))
                    if label:
                        out.append([{"label": label, "score": round(float(prob), 4)}])
                    else:
                        out.append([])
                return out
            except Exception as e:
                log.warning("BERTopic fit failed: %s", e)
                return [[] for _ in range(n)]

    # ------------------------------------------------------------------
    # contract
    # ------------------------------------------------------------------

    def tag_batch(
        self,
        items: list[dict],
        max_tags: int = 5,
    ) -> tuple[list[dict], str]:
        """Tag a document (its non-empty chunks).

        items: [{"id": int, "text": str, "lang": str|None}, ...]
        Returns (results, corpus_lang).
        """
        # Corpus-level language: explicit hint wins, else detect over the
        # joined text (LID is cheap even on long text).
        corpus_lang = "unknown"
        for it in items:
            if it.get("lang"):
                corpus_lang = str(it["lang"]).lower()
                break
        if corpus_lang == "unknown" and items:
            joined = " ".join(it["text"][:2000] for it in items[:5])
            corpus_lang = self._detect_lang(joined)

        texts = [it["text"] for it in items]
        topic_lists = self._fit_topics(texts)

        results: list[dict] = []
        for i, it in enumerate(items):
            text = it["text"]
            lang_hint = (it.get("lang") or "").lower() or corpus_lang
            entities = self._ner(text, lang_hint)
            keyphrases = self._keyphrases(text)
            topics = topic_lists[i]

            # Merge order: keyphrases (stable, cheap) first, then topic
            # labels as tags. Dedup is case-insensitive.
            tags: dict[str, None] = {}
            for t in keyphrases:
                tags.setdefault(t, None)
                if len(tags) >= max_tags:
                    break
            for t in topics:
                k = t["label"].lower()
                if k not in tags:
                    tags[k] = None
                    if len(tags) >= max_tags:
                        break

            results.append(
                {
                    "id": it["id"],
                    "tags": list(tags)[:max_tags],
                    "topics": topics,
                    "entities": entities,
                }
            )
        return results, corpus_lang

    # Backwards-compatible single-text entry (used by tests).
    def tag(self, text: str, lang: str | None = None, max_tags: int = 5) -> dict:
        results, _lang = self.tag_batch(
            [{"id": 0, "text": text, "lang": lang}], max_tags=max_tags
        )
        return results[0]
