#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${1:-bindery-demo}"
MODULE_NAME="${2:-core-web-client}"
LOCAL_PORT="${3:-8080}"

svc="$(kubectl -n "$NAMESPACE" get svc -l "bindery.platform/module=$MODULE_NAME" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -z "$svc" ]]; then
  echo "No Service found for module=$MODULE_NAME in namespace=$NAMESPACE" >&2
  kubectl -n "$NAMESPACE" get svc -l "bindery.platform/module=$MODULE_NAME" || true
  exit 1
fi

echo "Port-forwarding svc/$svc -> http://localhost:$LOCAL_PORT"
exec kubectl -n "$NAMESPACE" port-forward "svc/$svc" "${LOCAL_PORT}:8080"

