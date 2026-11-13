#!/usr/bin/env bash
# Search across all collections with a given query.
# Usage: ./search-all.sh "query" [limit]
set -euo pipefail

QUERY="${1:-}"
LIMIT="${2:-10}"

if [[ -z "$QUERY" ]]; then
  echo "Usage: $0 \"query\" [limit]"
  exit 1
fi

seek search "$QUERY" --lex -l "$LIMIT"
