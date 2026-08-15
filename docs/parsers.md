# Schema-Driven Parser Engine Guide

`seek` features an extensible, schema-driven parser engine that indexes conversation sessions and chat databases from various AI tools without requiring Go code modifications.

---

## 🤖 Built-in Parser Schemas

Run `seek parsers list` to check automatic discovery status:

| Schema | Driver | Default Source Path | Description |
|---|---|---|---|
| `opencode` | SQLite | `~/.local/share/opencode/opencode*.db` | Opencode CLI agent sessions |
| `copilot-cli` | SQLite | `~/.copilot/session-store.db` | GitHub Copilot CLI session history |
| `zed` | SQLite | `~/.local/share/zed/threads/threads.db` | Zed editor AI assistant panel threads (zstd compressed) |
| `claude` | JSONL | `~/.claude/projects/**/*.jsonl` | Claude Code conversations (text-only schema mode) |
| `codex` | JSONL | `~/.codex/sessions/**/*.jsonl` | Codex conversations (text-only schema mode) |

---

## ➕ Adding Parser Collections

```bash
# Add shortcuts for common agents:
seek add --opencode
seek add --copilot
seek add --zed

# Or add by schema name:
seek add --parser opencode
```

---

## 🌐 Workspace Filtering (`--workspace`)

Parser schemas extract workspace directory paths into a unified `workspace` fastfield:

```bash
# Filter agent conversations by target project directory:
seek search "deploy" --workspace /home/user/myproject
```

---

## 🛠️ User Custom Schemas & Overrides

Drop a YAML definition into `~/.config/seek/parsers/<name>.yaml` to define a new tool or override built-in behavior without waiting for a `seek` release.
