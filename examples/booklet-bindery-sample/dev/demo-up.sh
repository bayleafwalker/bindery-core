#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${1:-bindery}"
LOCAL_PORT="${2:-18080}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

cd "$REPO_ROOT"

TAG="${BINDERY_DEMO_TAG:-dev-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)-$(date +%s)}"
export BINDERY_DEMO_TAG="$TAG"

./k8s/dev/kind-up.sh "$CLUSTER_NAME"

if ! kubectl config current-context | grep -qx "kind-$CLUSTER_NAME"; then
  kubectl config use-context "kind-$CLUSTER_NAME" >/dev/null
fi

kubectl apply -f k8s/crds/

bash "$SCRIPT_DIR/controller-start.sh"
bash "$SCRIPT_DIR/build-images.sh" "$CLUSTER_NAME" "$TAG"
bash "$SCRIPT_DIR/apply.sh" "$TAG"

kubectl -n bindery-demo wait --for=condition=BindingsResolved=True worldinstance/bindery-sample-world --timeout=180s
kubectl -n bindery-demo wait --for=condition=RuntimeReady=True worldinstance/bindery-sample-world --timeout=240s
kubectl -n bindery-demo wait --for=condition=Available=True deployment -l bindery.platform/module=core-web-client --timeout=240s

echo "Demo is up (tag=$TAG). Port-forwarding web on http://localhost:$LOCAL_PORT"
exec bash "$SCRIPT_DIR/port-forward-web.sh" bindery-demo core-web-client "$LOCAL_PORT"

