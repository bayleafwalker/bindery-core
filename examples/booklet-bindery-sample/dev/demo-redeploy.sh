#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${1:-bindery}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

cd "$REPO_ROOT"

TAG="${BINDERY_DEMO_TAG:-dev-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)-$(date +%s)}"
export BINDERY_DEMO_TAG="$TAG"

if ! kubectl config current-context | grep -qx "kind-$CLUSTER_NAME"; then
  kubectl config use-context "kind-$CLUSTER_NAME" >/dev/null
fi

bash "$SCRIPT_DIR/build-images.sh" "$CLUSTER_NAME" "$TAG"
bash "$SCRIPT_DIR/apply.sh" "$TAG"

kubectl -n bindery-demo wait --for=condition=Available=True deployment -l bindery.platform/module=core-physics-engine --timeout=240s || true
kubectl -n bindery-demo wait --for=condition=Available=True deployment -l bindery.platform/module=core-web-client --timeout=240s || true

echo "Redeployed demo (tag=$TAG)."

