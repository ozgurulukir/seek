#!/usr/bin/env bash
# Check seek index status and collection health.
set -euo pipefail

echo "=== seek status ==="
seek status

echo ""
echo "=== parser schemas ==="
seek parsers list

echo ""
echo "=== binary location ==="
which seek || echo "seek not found in PATH"
