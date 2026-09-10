"""Tag pipeline for the seek semantic tag service.

Combines four components into the contract shape
``{"tags": [...], "topics": [...], "entities": [...]}``:

  - LID        fasttext-langdetect  (optional; degrades to "unknown")
  - NER        spaCy                (optional; degrades to no entities)
  - Keyphrase  YAKE                 (light default, always available)
  - Topics     BERTopic             (heavy; loaded lazily, first /tag call)

All optional heavy dependencies are imported lazily inside try/except so
that the service boots and answers with whatever is installed. Model
state is reported via ``model_status()`` so ``/health`` (and therefore
seek) can see which capabilities are active. No model output format is
ever passed through raw: everything is normalized to the contract
envelope before it reaches FastAPI.
"""

from __future__ import annotations

import logging
import threading

log = logging.getLogger("semantic")

# Topic labels produced by BERTopic can be long; keep the contract tidy.
MAX_TOPIC_LABEL_CHARS = 60


class TagPipeline:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._lid = None
        self._lid_failed = False
        self._spacy_nlp: dict | None = None
        self._spacy_failed = False
        self._bertopic = None
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
            "topic": self._bertopic is not None,
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

    def _get_bertopic(self):
        # Heavy: only built on first use. A fresh model per service start
        # is fine; fitting over all chunks happens inside _topics().
        if self._bertopic is None and not self._bertopic_failed:
            try:
                from sentence_transformers import SentenceTransformer

                from bertopic import BERTopic

                self._bertopic = BERTopic(
                    embedding_model=SentenceTransformer("paraphrase-multilingual-MiniLM-L12-v2"),
                )
            except Exception as e:
                self._bertopic_failed = True
                log.warning("BERTopic unavailable (%s); topics disabled", e)
        return self._bertopic

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
            if len(ents) >= 10:
                break
        return ents

    def _keyphrases(self, text: str, lang: str) -> list[str]:
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

    def _topics(self, text: str, lang: str, max_tags: int) -> list[dict]:
        model = self._get_bertopic()
        if model is None:
            return []
        with self._lock:
            try:
                topics, probs = model.transform(text)
            except Exception:
                return []
        best: list[tuple[float, str]] = []
        for prob, topic in zip(probs, topics):
            if topic < 0:  # -1 = outlier
                continue
            try:
                label = str(model.get_topic(topic))[:MAX_TOPIC_LABEL_CHARS]
            except Exception:
                continue
            if label and label != "-1":
                best.append((float(prob), label))
        best.sort(key=lambda x: x[0], reverse=True)
        return [
            {"label": label, "score": round(score, 4)} for score, label in best[:max_tags]
        ]

    # ------------------------------------------------------------------
    # contract
    # ------------------------------------------------------------------

    def tag(self, text: str, lang: str | None = None, max_tags: int = 5) -> dict:
        lang = (lang or "").lower() or self._detect_lang(text)
        entities = self._ner(text, lang)
        keyphrases = self._keyphrases(text, lang)
        topics = self._topics(text, lang, max_tags)

        # Merge order: keyphrases (stable, cheap) first, then topics as
        # tags (topical labels double as search tags). Entities stay a
        # separate facet; dedup is case-insensitive.
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

        return {
            "tags": list(tags)[:max_tags],
            "topics": topics,
            "entities": entities,
        }
