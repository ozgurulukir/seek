# Background Service

The background service automatically runs `seek sync` and `seek embed` periodically to keep your index up-to-date.

## Platform Support

| Platform | Implementation |
|----------|----------------|
| Linux | systemd user timer + oneshot service |
| macOS | launchd LaunchAgent plist |
| Windows | Task Scheduler (schtasks) |

## Commands

```bash
# Start service with default interval (1 hour = 3600 seconds)
seek service start

# Start with custom interval (e.g. 15 minutes)
seek service start -i 900

# Check service status
seek service status

# Stop and remove service
seek service stop
```

## What It Runs

The service executes two commands in sequence:

1. **`seek sync`** — Incrementally syncs new/changed files across all collections
2. **`seek embed`** — Generates embeddings for any chunks that don't have them yet

Both commands are idempotent — running them repeatedly is safe and only processes new work.

## Interval

The minimum interval is **60 seconds**. Values below 60 are automatically clamped to 60.

The default interval is **3600 seconds** (1 hour).

## Platform Details

### Linux (systemd)

- Creates: `~/.config/systemd/user/seek.service` and `seek.timer`
- Logs to: `~/.cache/seek/service.log`
- Uses: `systemctl --user daemon-reload`, `enable --now seek.timer`

### macOS (launchd)

- Creates: `~/Library/LaunchAgents/io.github.ethan-huo.seek.plist`
- Logs to: `~/.cache/seek/service.log`
- Service label: `io.github.ethan-huo.seek`
- Uses: `launchctl bootstrap` / `launchctl bootout`

### Windows (Task Scheduler)

- Task name: `SeekSync`
- Schedule type: `MINUTE` with configurable repetition
- Uses: `schtasks.exe /Create`, `/Query`, `/Delete`

## Viewing Logs

```bash
# On Linux/macOS:
tail -f ~/.cache/seek/service.log

# Or use:
seek status  # check current collection counts
```

## Troubleshooting

**Service not running?**

```bash
# Check status
seek service status

# Reinstall if needed
seek service stop
seek service start -i 3600
```

**systemd issues (Linux):**
```bash
systemctl --user status seek.timer
systemctl --user status seek.service
journalctl --user -u seek.service
```

**launchd issues (macOS):**
```bash
launchctl print gui/$UID/io.github.ethan-huo.seek
cat ~/Library/LaunchAgents/io.github.ethan-huo.seek.plist
```

**Task Scheduler (Windows):**
```powershell
schtasks /Query /TN SeekSync /FO LIST
```

## Service vs Hooks

The background service and hooks are complementary:

| Feature | Service | Hooks |
|---------|---------|-------|
| Frequency | Periodic (every N minutes) | On every Claude Code Stop event |
| Coverage | All collections | All collections (via `sync`) |
| Latency | Batched (up to interval) | Near real-time |
| Requires Claude Code? | No | Yes (hooks into `settings.json`) |

For best coverage, use **both**: hooks for immediate indexing after conversations, and service for background catch-up sync.
