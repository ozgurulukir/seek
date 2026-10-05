#!/usr/bin/env python3
"""Copy canonical optional service sources into the standalone seek skill."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
FILES = (
    ("tools/semantic/server.py", "skills/seek/scripts/services/semantic/server.py"),
    ("tools/semantic/tagger.py", "skills/seek/scripts/services/semantic/tagger.py"),
    ("tools/semantic/bootstrap_models.py", "skills/seek/scripts/services/semantic/bootstrap_models.py"),
    ("tools/semantic/test_server.py", "skills/seek/scripts/services/semantic/test_server.py"),
    ("tools/semantic/setup.sh", "skills/seek/scripts/services/semantic/setup.sh"),
    ("tools/semantic/setup.ps1", "skills/seek/scripts/services/semantic/setup.ps1"),
    ("tools/semantic/requirements.txt", "skills/seek/scripts/services/semantic/requirements.txt"),
    ("tools/semantic/vendor-LICENSES.md", "skills/seek/scripts/services/semantic/vendor-LICENSES.md"),
    ("tools/xberg_server/server.py", "skills/seek/scripts/services/xberg_server/server.py"),
    ("tools/flashrank_server/server.py", "skills/seek/scripts/services/flashrank_server/server.py"),
    ("tools/embed_server/server.py", "skills/seek/scripts/services/embed_server/server.py"),
)


def normalize(data: bytes) -> bytes:
    """Compare text ignoring line endings.

    The mirrored trees are pinned to LF in .gitattributes, but a checkout
    made before that rule (or an editor writing CRLF) must not raise a
    false drift alarm."""
    return data.replace(b"\r\n", b"\n")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="check for missing or stale bundled files")
    args = parser.parse_args()

    problems = []
    for source_name, target_name in FILES:
        source = ROOT / source_name
        target = ROOT / target_name
        if not source.is_file():
            problems.append(f"missing canonical source: {source_name}")
            continue
        contents = source.read_bytes()
        if args.check:
            if not target.is_file():
                problems.append(f"missing bundled copy: {target_name}")
            elif normalize(target.read_bytes()) != normalize(contents):
                problems.append(f"stale bundled copy: {target_name}")
        else:
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(contents)

    if problems:
        print("\n".join(problems), file=sys.stderr)
        return 1
    print("Skill service bundle is up to date." if args.check else "Skill service bundle synchronized.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
