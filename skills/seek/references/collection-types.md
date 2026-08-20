# Collection Types

| Type | Source | What's indexed |
|---|---|---|
| `code` | Any directory / repo | 35+ languages (Go, Rust, Python, TS, etc.) — `.gitignore`-aware, structural chunks + fastfields |
| `markdown` | Any directory | `.md` files — FTS + chunks + embeddings |
| `claude` | `~/.claude/projects/` | Claude Code conversations + screenshots |
| `codex` | `~/.codex/` | Codex sessions + screenshots |
| `images` | Any directory | Image files (png/jpg/webp) with VL embedding |
| `pdf` | Any directory | PDF pages rasterized to PNG, VL embedding per page + OCR text (if enabled) |
| `documents` | Any directory | Rich documents (docx/xlsx/pptx/epub/html/...) via extraction backend (builtin/xberg) |
| `parser` | External SQLite/JSONL | Schema-driven: opencode, copilot-cli, zed, claude (text-only), codex (text-only) |

## Source code collections (`code`)

Indexes source code repositories with automatic language detection, `.gitignore` filtering, vendor / node_modules / lockfile skipping, and binary detection.

```bash
seek add ~/projects/my-repo --code --name myrepo
```

## Rich documents collections (`documents`)

Indexes rich document formats via a configurable extraction backend.

**Backends:**
- `xberg` — 100+ formats (docx, xlsx, pptx, epub, html, eml, csv, etc.)
- `builtin` — only markdown, pdf, and images (no rich document extraction)

**Flags:**
- `--documents` — primary flag
- `--docs` — shortcut alias
- `--backend` — override the config default for this collection only

```bash
# Add rich documents (--documents is primary, --docs is alias)
seek add ~/docs --documents --name papers
seek add ~/docs --docs --name papers           # shortcut alias

# Override extraction backend
seek add ~/docs --documents --backend xberg     # use xberg service
seek add ~/docs --documents --backend builtin   # use builtin (limited)
```

**Note:** The builtin backend only supports markdown, PDF, and images. For docx/xlsx/pptx/epub/etc., use `--backend xberg` or set `extractor.backend: xberg` in your config.

## Schema-driven parsers

Some conversation platforms are indexed via declarative YAML schemas (no Go code per platform). These are `parser` collections — they support the `--workspace` filter for cross-platform project filtering.

### Adding collections

```bash
seek add ~/docs --docs         # rich documents (docx, xlsx, pptx, epub, html, etc.)
seek add --opencode            # opencode CLI sessions
seek add --copilot             # GitHub Copilot CLI sessions
seek add --zed                 # Zed Agent panel threads
seek add --claude-schema       # Claude conversations (text-only, no image extraction)
seek add --codex-schema        # Codex conversations (text-only, no image extraction)
seek add --parser <name>       # any parser schema by name
```

### Listing available schemas

```bash
seek parsers list          # shows all schemas, detection status, linked collections
```

### User overrides

Drop a YAML file in `~/.config/seek/parsers/<name>.yaml` to replace a built-in schema.

### Workspace filtering

Parser collections extract workspace metadata into a common field:

```bash
seek search "deploy" --workspace /home/user/myproject
```

Run `seek status` to see which collections the user has configured.
