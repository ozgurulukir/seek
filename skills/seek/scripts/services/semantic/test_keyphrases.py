"""Keyphrase quality tests: uv run --with yake tools/semantic/test_keyphrases.py."""

import unittest

from tagger import TagPipeline, _normalize_keyphrase


class KeyphraseTests(unittest.TestCase):
    def test_policy(self):
        for noise in ("err", "nil", "None", "OK", "ctx", "return nil", "123", "a", "io", "!!!"):
            with self.subTest(noise=noise):
                self.assertEqual(_normalize_keyphrase(noise), "")
        for raw, expected in (
            ("`CacheKey`", "cachekey"),
            (" cache_key() ", "cache_key"),
            ("HTTPServer", "httpserver"),
            ("errHandler", "errhandler"),
            ("connection  pool", "connection pool"),
            ("nil pointer", "nil pointer"),
            ("İşlem Kuyruğu", "işlem kuyruğu"),
            ("önbellek", "önbellek"),
        ):
            with self.subTest(raw=raw):
                self.assertEqual(_normalize_keyphrase(raw), expected)
        self.assertEqual(_normalize_keyphrase("x" * 41), "")

    def test_candidates_keep_rank_and_deduplicate_after_normalization(self):
        class Extractor:
            def extract_keywords(self, text):
                return [(word, 0.1) for word in (
                    "err", "`CacheKey`", "CACHEKEY", "nil", "cache_key",
                    "İşlem Kuyruğu", "return nil", "connection pool",
                )]

        pipeline = TagPipeline.__new__(TagPipeline)
        pipeline._yake = Extractor()
        self.assertEqual(pipeline._keyphrases("code"), [
            "cachekey", "cache_key", "işlem kuyruğu", "connection pool",
        ])

    def test_real_yake_code_and_turkish(self):
        pipeline = TagPipeline()
        self.assertIsNotNone(pipeline._get_yake())
        for text in (
            "err nil ctx err nil ctx return nil",
            "func CacheKey(cache_key string) string { return cache_key } "
            "// CacheKey manages connection pooling and cache invalidation.",
            "İşlem kuyruğu önbellek yönetimi sağlar. İşlem kuyruğu verileri saklar.",
        ):
            with self.subTest(text=text):
                tags = pipeline._keyphrases(text)
                self.assertTrue(all(tag == _normalize_keyphrase(tag) for tag in tags))
                if "CacheKey" in text or "İşlem" in text:
                    self.assertTrue(tags)
                else:
                    self.assertEqual(tags, [])


if __name__ == "__main__":
    unittest.main()
