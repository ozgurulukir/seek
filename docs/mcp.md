# MCP Server (`seek mcp`)

`seek mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io) server on stdio, so AI agents (Claude Code, Codex, or any MCP client) can query your personal index as tools instead of scraping terminal output.

## Tools

| Tool | Arguments | Returns |
|---|---|---|
| `seek_search` | `query` (required), `limit`, `lex`, `vec`, `collection` | JSON array of results — **same field names as `seek search --json`**: `chunk_id`, `document_id`, `seq`, `title`, `path`, `collection`, `content`, `content_kind`, `score`, `chunk_type`, `start_line`, `end_line` |
| `seek_status` | — | JSON array of `{name, type, documents, chunks}` |
| `seek_autocomplete` | `prefix` (required), `max` | `{query, suggestions[]}` |

`content_kind` tells the agent what `content` holds: `"full"` (whole chunk text, chunk-level vector hits) or `"snippet"` (40-token FTS excerpt, document-level BM25/hybrid hits). See the [JSON output](#) contract in the README for details.

## Wiring up your agent

`seek mcp` is a standard stdio MCP server, so any MCP client can register it.
The block is the same everywhere; only the config file and (for Zed) the
section key differ. Commands below assume `seek` is on `PATH`.

### Claude Code

Add to `~/.claude.json` (global) or a project's `.mcp.json`:

```json
{
  "mcpServers": {
    "seek": { "command": "seek", "args": ["mcp"] }
  }
}
```

Restart Claude Code; the tools appear as `seek_search` / `seek_status` / `seek_autocomplete`.
Alternatively `claude mcp add seek -- seek mcp`.

### Codex CLI

Add to `~/.codex/config.toml`:

```toml
[mcp_servers.seek]
command = "seek"
args = ["mcp"]
```

Or register it once with `codex mcp add seek -- seek mcp`.

### Cursor

Add to `~/.cursor/mcp.json` (user) or `.cursor/mcp.json` (project), then
restart and switch to **Agent mode** (tools don't show in plain chat):

```json
{
  "mcpServers": {
    "seek": { "command": "seek", "args": ["mcp"] }
  }
}
```

### Zed

Add to `.config/zed/settings.json`. Note Zed uses the **`context_servers`**
key, not `mcpServers`:

```json
{
  "context_servers": {
    "seek": { "command": "seek", "args": ["mcp"] }
  }
}
```

### VS Code / GitHub Copilot

Add to `.vscode/mcp.json`:

```json
{
  "servers": {
    "seek": { "type": "stdio", "command": "seek", "args": ["mcp"] }
  }
}
```

### Other agents

Many other editors/agents speak MCP with the same `mcpServers` block in their
own config path (e.g. **Cline** writes to `settings/cline_mcp_settings.json`
under the Code/VS Code global storage, **OpenCode** reads `opencode.json`).
These formats evolve quickly — check the agent's MCP docs if the exact path or
key differs from the examples above. The server itself is transport-stdio, so
a server config of `{ "command": "seek", "args": ["mcp"] }` is what every
client ultimately needs.

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
