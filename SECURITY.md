# Security Policy

## Supported versions

| Version | Supported          |
|---------|--------------------|
| latest tag (>= v0.2.x) | :white_check_mark: |
| older tags / `main` between releases | best effort |

Only the most recent tagged release receives security fixes. Upgrade before
reporting an issue against an older version.

## Reporting a vulnerability

**Do not open a public issue for security reports.**

Report privately via GitHub Security Advisories:
<https://github.com/ozgurulukir/seek/security/advisories/new>

If that is not possible, contact the maintainer directly (see the GitHub
profile of `ozgurulukir`). Include:

- seek version (`seek --version`) and platform
- reproduction steps or a minimal PoC
- affected component (installer, CLI, store, search, ...)

You will get an initial response within 7 days. We will keep you informed of
progress toward a fix and a coordinated disclosure date. Please keep details
confidential until a fixed release is published.

## Scope

In scope:

- `install.sh` and the release/supply-chain path (checksums, artifacts, tags)
- the `seek` binary: command surface, hook installation, service units
- local data handling: index database, config, cache permissions
- query/embedding data flow to external providers

Out of scope:

- vulnerabilities in third-party dependencies, report upstream and let us know
- social engineering of end users
- reports from automated scanners without a demonstrated impact path

## Data flow disclosure (relevant context)

Keyword (BM25/FTS5) search is fully local. If you configure embedding,
rerank, or OCR providers, chunk text, query text, images, or PDF pages are
sent to the configured endpoints — see `seek config` for the active
endpoints. No telemetry is collected or sent anywhere.
