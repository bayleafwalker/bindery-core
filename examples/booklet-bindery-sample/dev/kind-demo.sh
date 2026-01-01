#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${1:-bindery}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

cd "$REPO_ROOT"

./k8s/dev/kind-up.sh "$CLUSTER_NAME"

# Ensure kubectl context is set to kind-$CLUSTER_NAME.
if ! kubectl config current-context | grep -qx "kind-$CLUSTER_NAME"; then
  kubectl config use-context "kind-$CLUSTER_NAME" >/dev/null
fi

kubectl apply -f k8s/crds/

bash "$SCRIPT_DIR/build-images.sh" "$CLUSTER_NAME"
bash "$SCRIPT_DIR/apply.sh"

cat <<EOF
bindery-sample installed on kind cluster: $CLUSTER_NAME

Note: Bindery controllers must be running for the world to reconcile.
Run (from repo root) in another terminal:
  make run-controller

When the world is ready, open the web client:
  bash examples/booklet-bindery-sample/dev/port-forward-web.sh
  # then visit http://localhost:8080
EOF
