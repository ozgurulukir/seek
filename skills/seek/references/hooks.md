# AI Agent Hooks (Claude Code & Codex)

Hooks keep each agent's conversation collection fresh when that agent finishes a conversation (`Stop`), including interrupted Codex turns (`Interrupt`), and can add relevant local context before a prompt is submitted (`UserPromptSubmit`).

## Commands

```bash
# Install hooks for all supported agents (Claude Code + Codex)
seek hooks install

# Also keep semantic search current (uses the configured embedding provider)
seek hooks install --embed

# Install for only one agent
seek hooks install --claude
seek hooks install --codex

# Uninstall (same flag selection)
seek hooks uninstall
seek hooks uninstall --codex
seek hooks status
seek hooks doctor
```

## How It Works

| Agent | Config file | Event |
|-------|-------------|-------|
| Claude Code | `~/.claude/settings.json` | `Stop` + `UserPromptSubmit` |
| Codex | `~/.codex/hooks.json` | `Stop` + `Interrupt` + `UserPromptSubmit` |

Both agents use the same Claude-Code-style JSON hook schema:

```json
{ "hooks": { "Stop": [ { "matcher": "", "hooks": [ { "type": "command", "command": "..." } ] } ] } }
```

The installed hooks:
1. On `Stop`, run `seek hooks sync --agent <agent>` for only that agent's collections.
2. On Codex `Interrupt`, launch a detached `seek hooks sync --agent codex` worker so the sync survives Codex's short interrupt-hook timeout.
3. Debounce repeated syncs for 15 seconds and record the last outcome; inspect it with `seek hooks status`.
4. On `UserPromptSubmit`, search the local index and provide capped relevant context to the agent.
5. With `seek hooks install --embed`, the sync child also runs a realtime
   embedding pass for the same agent collection as part of that same sync.

## What Gets Modified

**Claude Code install** adds agent-scoped hook commands to `~/.claude/settings.json`:
```json
{
  "hooks": {
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/seek hooks sync --agent claude",
            "timeout": 600,
            "statusMessage": "Syncing seek index..."
          }
        ]
      }
    ]
  }
}
```

**Codex install** writes the same event shape to `~/.codex/hooks.json`. Its commands
use `seek hooks sync --agent codex`, a background `Interrupt` launcher, and
`seek hooks context --agent codex`. The hook responses remain valid JSON; sync
failures are also returned through the process exit status and recorded for
`seek hooks status`.

**Uninstall removes** only the Seek command from its matching hook entry. Other
commands and hooks are preserved.

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
  - Periodic embedding, unless hooks were installed with `--embed`
  - Catch-up sync for code/markdown collections modified outside agents
  - Agents without hook support (opencode, copilot-cli, zed — use the service for those)

## Troubleshooting

**Hook not running?**

```bash
# Verify configuration and the last hook outcome
seek hooks doctor
seek hooks status

# Check that seek is in PATH for the agent environment
which seek

# Codex requires reviewing/trusting a newly installed or changed hook.
# In Codex, open /hooks and trust the Seek Interrupt hook if prompted.

# Try manually running the Claude hook action
seek hooks sync --agent claude
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

- `seek hooks install` leaves a current hook unchanged, but upgrades older direct, shell-wrapped, or unscoped hooks in place to agent-scoped `seek hooks sync --agent <agent>` commands.
- For Codex, re-running `seek hooks install --codex` also installs/upgrades the `Interrupt` hook and its detached background launcher.
- `seek hooks uninstall` removes only the Seek command from its matching hook entry. Other commands and hooks in the config files are preserved.
- If the `Stop` list becomes empty after uninstall, the event key is removed from the file.
