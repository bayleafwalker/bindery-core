#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${1:-bindery}"
TAG="${2:-${BINDERY_DEMO_TAG:-0.1.0}}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

cd "$REPO_ROOT"

KIND_BIN="kind"
if ! command -v kind >/dev/null 2>&1; then
  if [[ -x "./.tools/kind" ]]; then
    KIND_BIN="./.tools/kind"
  else
    echo "kind is required (install it or place it at ./.tools/kind)" >&2
    exit 1
  fi
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

echo "Building demo module images (tag=$TAG)..."
docker build -t "bindery/demo-physics:$TAG" -f examples/booklet-bindery-sample/cmd/demo-physics-module/Dockerfile .
docker build -t "bindery/demo-web:$TAG" -f examples/booklet-bindery-sample/cmd/demo-web-module/Dockerfile .

echo "Loading demo images into kind cluster: ${CLUSTER_NAME}"
"$KIND_BIN" load docker-image --name "$CLUSTER_NAME" \
  "bindery/demo-physics:$TAG" \
  "bindery/demo-web:$TAG"

echo "Demo images ready."
