#!/bin/sh
# check-stale-commands.sh — fail if README/docs/skills reference a seek command
# that no longer exists in the kong surface.
#
# This is a POSIX-sh script intended to run on CI (GitHub Actions / Linux). It
# is NOT runnable directly from the Windows dev shell (it relies on GNU
# sort/awk/sed); invoke it via bash, e.g.:
#
#     bash scripts/check-stale-commands.sh
#
# Canonical command set: derived from the golden help snapshot
# testdata/help/top-level.txt, which is rendered from the real kong grammar in
# main.go (regenerate with `go test -update .`). Deriving from the snapshot
# means there is no separate list to drift — the check and the CLI surface share
# one source of truth.
#
# Matching is restricted to real invocations so prose is not mistaken for a
# command:
#   - code spans: only spans that START with `seek  (e.g. `seek search "q"`), so
#     prose spans like `Syncing seek index...` are ignored;
#   - fenced code blocks: only lines inside ``` / ~~~ blocks, so prose sentences
#     like "seek also bypasses..." (which live in paragraphs) are ignored.
# Candidates not in the canonical set are reported as stale. A small denylist
# covers tokens that follow `seek` but are not commands (group names, prose).
#
# Scans README.md, docs/*.md (plans/ is excluded via maxdepth 1 — those docs
# describe future flag forms), and skills/**/*.md.
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
golden="$repo_root/testdata/help/top-level.txt"

if [ ! -f "$golden" ]; then
  echo "check-stale-commands: golden snapshot not found at $golden" >&2
  echo "regenerate it with: go test -tags \"fts5 sqlite_fts5\" -update ." >&2
  exit 2
fi

canonical=$(mktemp)
candidates=$(mktemp)
cand_sorted=$(mktemp)
deny=$(mktemp)
trap 'rm -f "$canonical" "$candidates" "$cand_sorted" "$deny"' EXIT

# Canonical top-level command names, from the golden snapshot's "Commands:"
# entries (the 2-space indented lines under "Commands:").
LC_ALL=C grep -E '^  [a-z][a-z0-9-]+ ' "$golden" \
  | LC_ALL=C awk '{print $1}' | LC_ALL=C sort -u > "$canonical"

# Tokens that follow `seek` but are not commands (group names, prose). Sorted
# for comm. Add new entries here when a doc legitimately references a non-command
# token after `seek`.
#
# `advanced` used to be denylisted (the `seek advanced` group name is not itself
# a command). It is now canonical: the golden's `advanced schema` /
# `advanced parsers list` / `advanced analyze` entries make `advanced` a first
# field of the derived canonical set, so the denylist entry is redundant.
: > "$deny"

# extract FILE — emit the command token for each real seek invocation in FILE:
#   (a) lines inside fenced code blocks (``` / ~~~);
#   (b) code spans that start with `seek .
extract() {
  f=$1
  [ -f "$f" ] || return 0
  # (b) code spans anchored at `seek  (prose spans like `Syncing seek index`
  #     start with something else and are ignored).
  grep -oE '`seek [A-Za-z][A-Za-z0-9_-]*' "$f" 2>/dev/null \
    | sed -E 's/`seek[[:space:]]+([A-Za-z][A-Za-z0-9_-]*)/\1/' || true
  # (a) lines inside fenced code blocks.
  awk '
    function emit(line,   s) {
      s = line
      sub(/^[ \t]+/, "", s)
      if (substr(s, 1, 1) == "$") { s = substr(s, 2); sub(/^[ \t]+/, "", s) }
      if (s ~ /^seek[ \t]+[A-Za-z]/) {
        sub(/^seek[ \t]+/, "", s)
        sub(/[ \t].*$/, "", s)
        sub(/[^A-Za-z0-9_-].*$/, "", s)
        if (s != "") print s
      }
    }
    BEGIN { incode = 0 }
    {
      if ($0 ~ /^[ \t]*(```+|~~~+)/) incode = !incode
      if (incode) emit($0)
    }
  ' "$f" || true
}

# Scan README.md, docs/*.md (plans/ excluded by maxdepth 1) and skills/**/*.md.
{
  echo "$repo_root/README.md"
  find "$repo_root/docs" -maxdepth 1 -type f -name '*.md' 2>/dev/null
  find "$repo_root/skills" -type f -name '*.md' 2>/dev/null
} | while IFS= read -r f; do
  extract "$f"
done > "$candidates"

LC_ALL=C sort -u "$candidates" > "$cand_sorted"

# Stale: candidate commands that are neither canonical nor in the denylist.
stale=$(LC_ALL=C comm -23 "$cand_sorted" "$canonical" | LC_ALL=C comm -23 - "$deny")

if [ -n "$stale" ]; then
  echo "ERROR: stale seek command references (not in the kong surface):" >&2
  printf '%s\n' "$stale" | while IFS= read -r c; do echo "  seek $c" >&2; done
  cat >&2 <<'EOF'
Fix one of:
  - update the doc/skill to a command that exists, or
  - if the command is new, add it to main.go and regenerate the golden
    snapshot with: go test -tags "fts5 sqlite_fts5" -update .
EOF
  exit 1
fi

n=$(LC_ALL=C grep -c . "$candidates" || true)
echo "check-stale-commands: OK — $n seek command reference(s) all resolve to known commands."
