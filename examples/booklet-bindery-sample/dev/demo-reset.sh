#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-18080}"

curl -sS -X POST "http://127.0.0.1:${PORT}/api/reset" | (command -v jq >/dev/null 2>&1 && jq . || cat)
echo

