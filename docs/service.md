# Background Automation, Service & Concurrency

`seek` is designed to maintain your index in the background automatically without interfering with interactive searches or coding agent workflows.

---

## 🕒 Background Service (`seek service`)

The background service periodically executes `seek sync` — which now also
embeds any newly indexed chunks in the same pass — using native operating
system task schedulers:

- **Windows**: Windows Task Scheduler (`schtasks.exe`)
- **Linux**: `systemd` user timer (`systemctl --user`)
- **macOS**: `launchd` agent plist (`~/Library/LaunchAgents/`)

### Commands:
```bash
seek service start              # start scheduled sync+embed (default: every 60 min)
seek service start -i 1800      # custom interval (every 30 minutes)
seek service status             # check current service status and next scheduled run
seek service stop               # stop and remove scheduled task
```

---

## 🪝 AI Agent Conversation Hooks (`seek hooks`)

Automatically sync conversations as soon as an AI agent finishes:

```bash
seek hooks install              # writes Stop/Interrupt + prompt-context hooks for Claude Code + Codex
seek hooks install --embed      # also refresh semantic embeddings after each agent sync
seek hooks install --claude     # only ~/.claude/settings.json
seek hooks install --codex      # only ~/.codex/hooks.json
seek hooks uninstall            # (--claude / --codex to select)
seek hooks status               # installed hooks and last hook-sync result
seek hooks doctor               # also validate the binary recorded in each hook
```

Each Stop hook invokes `seek hooks sync --agent <claude|codex>`, so it syncs
only that agent's conversation collections. Codex also gets an `Interrupt` hook
that quickly launches a detached background sync, because Codex limits
interrupt hooks to three seconds. Hook syncs are lock-protected and debounced
for 15 seconds; `seek hooks status` reports the last result. Prompt hooks run a
small local lexical search and return capped relevant context. Codex commands
always return valid JSON, while sync failures are recorded and surfaced through
the hook process status. Re-running install upgrades older hooks.

Claude Code runs the same JSON-safe wrapper with a ten-minute timeout and the
status message `Syncing seek index...` while it runs.

When Claude Code or Codex finishes a session, `seek sync` runs automatically to parse and index the conversation immediately.

---

## ⚡ Concurrency & SQLite WAL Architecture

`seek` uses SQLite with **Write-Ahead Logging (WAL)** mode (`_journal_mode=WAL`) and a multi-process busy timeout (`_busy_timeout=5000`):

1. **Non-blocking Concurrent Searches:**
   Reads and searches **never block and are never blocked by writes**. You can run as many concurrent `seek search` commands as you want even while a massive `seek sync` or `seek embed` is actively writing to the database.
2. **Safe Write Queuing:**
   If `seek sync` and `seek embed` run concurrently, SQLite's busy timeout allows them to queue safely without returning `database is locked` errors.
3. **Chunk Compression:**
   Chunks are stored with Zstd compression (`compression.algorithm: zstd`), saving disk space while allowing transparent decompress-on-read during search.
