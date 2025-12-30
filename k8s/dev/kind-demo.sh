#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${1:-anvil}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

cd "$REPO_ROOT"

"$SCRIPT_DIR/kind-up.sh" "$CLUSTER_NAME"

# Ensure kubectl context is set to kind-$CLUSTER_NAME.
if ! kubectl config current-context | grep -qx "kind-$CLUSTER_NAME"; then
  kubectl config use-context "kind-$CLUSTER_NAME" >/dev/null
fi

"$SCRIPT_DIR/apply-crds-and-examples.sh"

echo "Demo installed on kind cluster: $CLUSTER_NAME"
