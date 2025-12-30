#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${1:-anvil}"

KIND_BIN="kind"
if ! command -v kind >/dev/null 2>&1; then
  if [[ -x "./.tools/kind" ]]; then
    KIND_BIN="./.tools/kind"
  else
    echo "kind is not installed (and ./.tools/kind not found)" >&2
    exit 1
  fi
fi

"$KIND_BIN" delete cluster --name "$CLUSTER_NAME"

echo "kind cluster deleted: $CLUSTER_NAME"
