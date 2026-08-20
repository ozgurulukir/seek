# Claude Code Hooks

Hooks automatically run `seek sync` every time Claude Code finishes a conversation (`Stop` event). This keeps your conversation history indexed in near real-time.

## Commands

```bash
# Install the hook
seek hooks install

# Uninstall the hook
seek hooks uninstall
```

## How It Works

Hooks modify Claude Code's `settings.json` at `~/.claude/settings.json`.

The installed hook:
1. Triggers on the `Stop` event (when a Claude Code conversation ends)
2. Runs `seek sync` (incremental sync across all collections)
3. Any new Claude conversations are immediately available for search

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
1. `os.Executable()` (if available, i.e. running from the binary itself)
2. `exec.LookPath("seek")` (looks in `$PATH`)
3. Falls back to just `"seek"` if neither works

The path is stored as the resolved absolute path (evaluating symlinks) so the hook works regardless of how Claude Code is invoked.

## Hooks vs Service

| Feature | Hooks | Service |
|---------|-------|---------|
| Trigger | On every Claude Code Stop | Periodic (every N minutes) |
| Latency | Near real-time | Batched |
| Requires Claude Code? | Yes | No |
| Coverage | Runs `sync` only | Runs `sync && embed` |

**Recommended setup: both!**
- Use **hooks** for immediate indexing after Claude Code conversations
- Use **service** for:
  - Periodic `embed` (hooks only run `sync`)
  - Catch-up sync for code/markdown collections modified outside Claude Code
  - Systems without Claude Code (you only use Codex, opencode, or other agents)

## Troubleshooting

**Hook not running?**

```bash
# Verify hook is installed
cat ~/.claude/settings.json | grep -A10 "seek sync"

# Check that seek is in PATH for the Claude Code environment
which seek

# Try manually running what the hook runs
seek sync
```

**Hook was installed but isn't there anymore?**

Claude Code sometimes rewrites `settings.json`. Re-install:
```bash
seek hooks install
```

**Need to see the full settings?**
```bash
seek config          # check general config
cat ~/.claude/settings.json
```

## Idempotency

- `seek hooks install` checks if the hook already exists first. If found, it prints "Claude Code hook already installed." and exits without modification.
- `seek hooks uninstall` only removes entries containing `"seek sync"`. Other hooks in your `settings.json` are preserved.
