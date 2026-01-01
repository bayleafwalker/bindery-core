#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${1:-bindery}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

cd "$REPO_ROOT"

if [[ ! -f "examples/booklet-bindery-sample/dev/kind-demo.sh" ]]; then
  echo "Sample game not found at examples/booklet-bindery-sample; nothing to demo." >&2
  exit 1
fi

bash "examples/booklet-bindery-sample/dev/kind-demo.sh" "$CLUSTER_NAME"
