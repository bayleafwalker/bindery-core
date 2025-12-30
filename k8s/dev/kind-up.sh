#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${1:-anvil}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required" >&2
  exit 1
fi

KIND_BIN="kind"
if ! command -v kind >/dev/null 2>&1; then
  if [[ -x "./.tools/kind" ]]; then
    KIND_BIN="./.tools/kind"
  else
    cat >&2 <<'EOF'
kind is not installed.

Install kind (recommended) or place it at ./.tools/kind.

Linux amd64 example:
  curl -Lo ./.tools/kind https://kind.sigs.k8s.io/dl/v0.23.0/kind-linux-amd64
  chmod +x ./.tools/kind

Linux arm64 example:
  curl -Lo ./.tools/kind https://kind.sigs.k8s.io/dl/v0.23.0/kind-linux-arm64
  chmod +x ./.tools/kind
EOF
    exit 1
  fi
fi

# Create cluster if it doesn't exist
if "$KIND_BIN" get clusters | grep -qx "$CLUSTER_NAME"; then
  echo "kind cluster already exists: $CLUSTER_NAME"
else
  "$KIND_BIN" create cluster --name "$CLUSTER_NAME"
fi

kubectl cluster-info >/dev/null

echo "kind cluster ready: $CLUSTER_NAME"
