# AI Agent Hooks (Claude Code & Codex)

Hooks automatically run `seek sync` every time an AI coding agent finishes a conversation (`Stop` event). This keeps your conversation history indexed in near real-time.

## Commands

```bash
# Install hooks for all supported agents (Claude Code + Codex)
seek hooks install

# Install for only one agent
seek hooks install --claude
seek hooks install --codex

# Uninstall (same flag selection)
seek hooks uninstall
seek hooks uninstall --codex
```

## How It Works

| Agent | Config file | Event |
|-------|-------------|-------|
| Claude Code | `~/.claude/settings.json` | `Stop` |
| Codex | `~/.codex/hooks.json` | `Stop` |

Both agents use the same Claude-Code-style JSON hook schema:

```json
{ "hooks": { "Stop": [ { "matcher": "", "hooks": [ { "type": "command", "command": "..." } ] } ] } }
```

The installed hook:
1. Triggers on the `Stop` event (when a Claude Code / Codex conversation ends)
2. Runs `seek sync` (incremental sync across all collections)
3. Any new conversations are immediately available for search

## What Gets Modified

**Install adds** to `hooks.Stop` array in `settings.json`:
```json
{
  "hooks": {
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/seek sync"
          }
        ]
      }
    ]
  }
}
```

**Uninstall removes** only the seek-specific entry (identified by `"seek sync"` in the command). Other hooks are preserved.

## Binary Resolution

The hook finds the `seek` binary using:
1. `exec.LookPath("seek")` (looks in `$PATH`)
2. Falls back to just `"seek"` if not found

The path is stored as the resolved absolute path (evaluating symlinks) so the hook works regardless of how the agent is invoked.

## Hooks vs Service

| Feature | Hooks | Service |
|---------|-------|---------|
| Trigger | On every Claude Code / Codex Stop | Periodic (every N minutes) |
| Latency | Near real-time | Batched |
| Requires Claude Code? | Yes | No |
| Coverage | Runs `sync` only | Runs `sync && embed` |

**Recommended setup: both!**
- Use **hooks** for immediate indexing after Claude Code / Codex conversations
- Use **service** for:
  - Periodic `embed` (hooks only run `sync`)
  - Catch-up sync for code/markdown collections modified outside agents
  - Agents without hook support (opencode, copilot-cli, zed — use the service for those)

## Troubleshooting

**Hook not running?**

```bash
# Verify hook is installed
cat ~/.claude/settings.json | grep -A10 "seek sync"
cat ~/.codex/hooks.json | grep -A10 "seek sync"

# Check that seek is in PATH for the agent environment
which seek

# Try manually running what the hook runs
seek sync
```

**Hook was installed but isn't there anymore?**

Agents sometimes rewrite their config files. Re-install:
```bash
seek hooks install
```

**Need to see the full settings?**
```bash
seek config          # check general config
cat ~/.claude/settings.json
cat ~/.codex/hooks.json
```

## Idempotency

- `seek hooks install` checks if the hook already exists first (per agent). If found, it prints "<agent> hook already installed." and exits without modification.
- `seek hooks uninstall` only removes entries containing `"seek sync"`. Other hooks in the config files are preserved.
- If the `Stop` list becomes empty after uninstall, the event key is removed from the file.
