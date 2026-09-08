# MCP Server (`seek mcp`)

`seek mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io) server on stdio, so AI agents (Claude Code, Codex, or any MCP client) can query your personal index as tools instead of scraping terminal output.

## Tools

| Tool | Arguments | Returns |
|---|---|---|
| `seek_search` | `query` (required), `limit`, `lex`, `vec`, `collection` | JSON array of results — **same field names as `seek search --json`**: `chunk_id`, `document_id`, `seq`, `title`, `path`, `collection`, `content`, `content_kind`, `score`, `chunk_type`, `start_line`, `end_line` |
| `seek_status` | — | JSON array of `{name, type, documents, chunks}` |
| `seek_autocomplete` | `prefix` (required), `max` | `{query, suggestions[]}` |

`content_kind` tells the agent what `content` holds: `"full"` (whole chunk text, chunk-level vector hits) or `"snippet"` (40-token FTS excerpt, document-level BM25/hybrid hits). See the [JSON output](#) contract in the README for details.

## Wiring up Claude Code

Add to `~/.claude.json` (global) or a project's `.mcp.json`:

```json
{
  "mcpServers": {
    "seek": { "command": "seek", "args": ["mcp"] }
  }
}
```

Restart Claude Code; the tools appear as `seek_search` / `seek_status` / `seek_autocomplete`.

## Raw protocol probe

```bash
seek mcp <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"seek_status","arguments":{}}}
EOF
```

## Privacy

`seek mcp` reads only the local SQLite index. It performs the same network calls as `seek search` — i.e. embedding requests only when a vector search actually runs, honoring `privacy.offline_only` (with `offline_only: true`, vector legs fail closed exactly like the CLI).
