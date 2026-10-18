# Documentation Index

Welcome to the `seek` documentation. `seek` is a personal hybrid search engine
(BM25 full-text + vector semantic search) for markdown notes, source code, and
AI agent conversations, backed by SQLite (FTS5) with optional local or cloud
embeddings.

## Getting started

- [100% Local & Offline Setup](local-setup.md) — Ollama, FlashRank, semantic tagging, and xberg setup, with the local architecture flow.
- [Models & Re-ranking Guide](models.md) — embedding and cross-encoder comparisons, dimension rules, CPU latency benchmarks.
- [Document Extractors & OCR](extractors.md) — builtin vs xberg extraction (100+ formats) and OCR vision.

## Usage

- [Query Syntax & Filters Guide](query-guide.md) — structured AST syntax, filters, line spans, and context expansion.
- [Schema-Driven Parsers](parsers.md) — opencode, Copilot CLI, Zed, Hermes, and ZCode conversation sources.
- [Semantic Tag Service](semantic.md) — optional local NLP service generating fast fields for all text-bearing collections.
- [MCP Server](mcp.md) — `seek mcp` tools for Claude Code and other agents.

## Operations

- [Background Service & Concurrency](service.md) — scheduled tasks (Task Scheduler / systemd / launchd), hooks, SQLite WAL.
- [OCR Provider Compatibility](ocr-provider-compatibility.md) — OCR provider notes.
- [Baidu OCR](baidu-ocr.md) — Baidu OCR integration notes.

## Reference

- [CLI Inventory & Command Classification](cli-inventory.md) — canonical command surface and classification.
- [Architecture Decision Records](adr/0001-cli-drop-decisions.md) — CLI drop decisions.
- [Release notes](releases/) — per-version release notes.

## Other

- [README](../README.md) — project overview, quickstart, and features.
- [Agent skill reference](../skills/seek/SKILL.md) — agent prompt instructions and CLI reference.