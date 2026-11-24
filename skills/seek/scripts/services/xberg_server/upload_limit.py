"""Bounded upload reading for the xberg-compatible extract service.

This module is deliberately stdlib-only so the byte-cap logic can be unit
tested without fastapi or the document-parsing libraries installed.
"""

from __future__ import annotations

import os
import tempfile
from typing import BinaryIO, Protocol

DEFAULT_MAX_UPLOAD_BYTES = 100 * 1024 * 1024  # 100 MiB
UPLOAD_CHUNK_BYTES = 1024 * 1024  # 1 MiB per read


class AsyncReader(Protocol):
    """The part of starlette's UploadFile that the bounded reader needs."""

    size: int | None

    async def read(self, size: int = -1) -> bytes: ...


def positive_env_int(name: str, default: int) -> int:
    """Read a positive int from the environment, falling back on invalid input."""
    try:
        value = int(os.getenv(name, str(default)))
    except (TypeError, ValueError):
        return default
    return value if value > 0 else default


MAX_UPLOAD_BYTES = positive_env_int("XBERG_MAX_UPLOAD_BYTES", DEFAULT_MAX_UPLOAD_BYTES)


class UploadTooLargeError(Exception):
    """Raised when an upload stream exceeds the configured byte limit."""

    def __init__(self, size: int, limit: int) -> None:
        super().__init__(f"uploaded file is {size} bytes; limit is {limit} bytes")
        self.size = size
        self.limit = limit


async def read_upload_limited(
    upload: AsyncReader,
    limit: int = MAX_UPLOAD_BYTES,
    chunk_size: int = UPLOAD_CHUNK_BYTES,
) -> BinaryIO:
    """Stream an upload into a spooled temp file, enforcing ``limit``.

    Content-Length (``upload.size``) is advisory and is checked by the caller;
    this function counts the bytes actually read so a lying or missing header
    cannot bypass the cap. On success it returns an open spooled file seeked
    to 0 — the caller owns it and must close it. On failure the temp file is
    closed before the exception propagates.
    """
    spooled = tempfile.SpooledTemporaryFile(max_size=chunk_size)
    total = 0
    try:
        while True:
            chunk = await upload.read(chunk_size)
            if not chunk:
                break
            total += len(chunk)
            if total > limit:
                raise UploadTooLargeError(total, limit)
            spooled.write(chunk)
        spooled.seek(0)
        return spooled
    except BaseException:
        spooled.close()
        raise
