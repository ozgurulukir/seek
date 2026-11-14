"""Download and verify optional models used by the full semantic service."""

from __future__ import annotations


def install_spacy_model(name: str, *, optional: bool = False) -> bool:
    import spacy

    try:
        spacy.load(name)
        print(f"    {name}: already present")
        return True
    except OSError:
        pass

    try:
        spacy.cli.download(name)
        spacy.load(name)
        print(f"    {name}: installed")
        return True
    except BaseException as exc:
        label = "optional; skipped" if optional else "FAILED"
        print(f"    {name}: {label} ({type(exc).__name__}: {exc})")
        return False


def verify_lid() -> bool:
    try:
        try:
            import ftlangdetect

            result = ftlangdetect.detect("Bu bir Türkçe dil algılama testidir.")
            lang = result["lang"]
        except ImportError:
            from fasttext_langdetect import LangDetector

            lang, _score = LangDetector().detect(
                "Bu bir Türkçe dil algılama testidir."
            )
        print(f"    LID vectors: ready (detected {lang})")
        return True
    except Exception as exc:
        print(f"    LID vectors: FAILED ({type(exc).__name__}: {exc})")
        return False


def verify_topic_stack() -> bool:
    try:
        import bertopic  # noqa: F401
        import sentence_transformers  # noqa: F401

        print("    BERTopic + sentence-transformers: ready")
        return True
    except Exception as exc:
        print(f"    topic stack: FAILED ({type(exc).__name__}: {exc})")
        return False


def main() -> int:
    print("==> Downloading spaCy models")
    ner_ready = install_spacy_model("xx_ent_wiki_sm")
    install_spacy_model("tr_core_news_sm", optional=True)

    print("==> Downloading/verifying LID model")
    lid_ready = verify_lid()

    print("==> Verifying topic stack")
    topic_ready = verify_topic_stack()

    if not (ner_ready and lid_ready and topic_ready):
        print("error: one or more required full-pipeline capabilities are unavailable")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
