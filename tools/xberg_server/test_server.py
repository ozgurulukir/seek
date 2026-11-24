"""Tests for the xberg-compatible extract service upload cap.

The byte-limiting helper is stdlib-only and is always tested. The HTTP-layer
tests need fastapi/httpx plus the document parsers; they are skipped when those
optional dependencies are not installed.
"""

import asyncio
import os
import unittest
from unittest.mock import patch

from upload_limit import (
    DEFAULT_MAX_UPLOAD_BYTES,
    MAX_UPLOAD_BYTES,
    UploadTooLargeError,
    positive_env_int,
    read_upload_limited,
)


class FakeUpload:
    """Minimal async upload that yields queued chunks and honors read(n)."""

    def __init__(self, chunks, size=None):
        self._chunks = list(chunks)
        self.size = size
        self.reads = 0

    async def read(self, size=-1):
        self.reads += 1
        if not self._chunks:
            return b""
        chunk = self._chunks.pop(0)
        if size > 0 and len(chunk) > size:
            self._chunks.insert(0, chunk[size:])
            chunk = chunk[:size]
        return chunk


class ReadUploadLimitedTests(unittest.TestCase):
    def test_accepts_upload_within_limit(self):
        async def run():
            upload = FakeUpload([b"hello ", b"world"], size=11)
            spooled = await read_upload_limited(upload, limit=100)
            try:
                self.assertEqual(spooled.read(), b"hello world")
            finally:
                spooled.close()

        asyncio.run(run())

    def test_accepts_upload_exactly_at_limit(self):
        async def run():
            upload = FakeUpload([b"abc", b"def"], size=6)
            spooled = await read_upload_limited(upload, limit=6, chunk_size=2)
            try:
                self.assertEqual(spooled.read(), b"abcdef")
            finally:
                spooled.close()

        asyncio.run(run())

    def test_rejects_oversized_upload(self):
        async def run():
            upload = FakeUpload([b"a" * 6, b"b" * 6], size=12)
            with self.assertRaises(UploadTooLargeError) as ctx:
                await read_upload_limited(upload, limit=10, chunk_size=4)
            self.assertEqual(ctx.exception.size, 12)
            self.assertEqual(ctx.exception.limit, 10)
            self.assertIn("10 bytes", str(ctx.exception))

        asyncio.run(run())

    def test_stops_reading_at_cap(self):
        async def run():
            # chunk_size=4, limit=10: the third read brings the total to 12,
            # so the fourth queued chunk must never be consumed.
            upload = FakeUpload([b"aaaa", b"bbbb", b"cccc", b"dddd"])
            with self.assertRaises(UploadTooLargeError):
                await read_upload_limited(upload, limit=10, chunk_size=4)
            self.assertEqual(upload.reads, 3)

        asyncio.run(run())

    def test_env_limit_validation(self):
        for raw, want in (("2048", 2048), ("0", 100), ("-5", 100), ("garbage", 100)):
            with patch.dict(os.environ, {"XBERG_MAX_UPLOAD_BYTES": raw}):
                self.assertEqual(positive_env_int("XBERG_MAX_UPLOAD_BYTES", 100), want)
        self.assertGreater(MAX_UPLOAD_BYTES, 0)
        self.assertEqual(DEFAULT_MAX_UPLOAD_BYTES, 100 * 1024 * 1024)


try:
    from fastapi.testclient import TestClient

    import server as server_module

    client = TestClient(server_module.app)
except Exception:  # optional dependencies absent — helper tests still run
    client = None
    server_module = None


@unittest.skipUnless(client is not None, "fastapi/httpx/parsers not installed")
class ExtractHTTPTests(unittest.TestCase):
    def test_oversized_content_length_returns_413(self):
        with patch.object(server_module, "MAX_UPLOAD_BYTES", 8):
            response = client.post(
                "/extract",
                files={"file": ("big.txt", b"x" * 16, "text/plain")},
            )
        self.assertEqual(response.status_code, 413)
        self.assertIn("exceeds", response.json()["detail"])

    def test_stream_cap_returns_413(self):
        async def too_large(*_args, **_kwargs):
            raise UploadTooLargeError(100, 8)

        with patch.object(server_module, "read_upload_limited", new=too_large):
            response = client.post(
                "/extract",
                files={"file": ("note.txt", b"hello world", "text/plain")},
            )
        self.assertEqual(response.status_code, 413)

    def test_accepted_upload_parses(self):
        response = client.post(
            "/extract",
            files={"file": ("note.txt", b"hello world", "text/plain")},
        )
        self.assertEqual(response.status_code, 200)
        body = response.json()
        self.assertEqual(body["results"][0]["content"], "hello world")
        self.assertEqual(body["summary"], {"inputs": 1, "results": 1, "errors": 0})


if __name__ == "__main__":
    unittest.main()
