#!/usr/bin/env bash
#
# Verifies that the checked-in CRD manifests stay aligned with the Go API types.
#
# This enforces two invariants:
#
#   1. Every kind registered into the scheme has a CRD manifest in k8s/crds/.
#      Without this, the manager registers a controller for a kind the API
#      server has never heard of, its informer never syncs, and the process
#      exits on a cache-sync timeout. That is exactly how the ShardAutoscaler
#      CRD went missing and broke the e2e smoke test.
#
#   2. k8s/crds/ and helm/bindery-core/crds/ hold identical manifests, so a
#      Helm install and a `kubectl apply -f k8s/crds/` agree.
#
# The first check is deliberately one-directional. A CRD manifest with no Go
# type is legitimate here: CapabilityDefinition is applied declaratively by the
# sample game and granted in RBAC, but has no controller and no compiled type.
# Requiring a Go type for every manifest would flag it as an error.
#
# This does NOT compare schema contents. The hand-written manifests currently
# carry more validation than the Go markers generate (see
# docs/standards/kubernetes/crds.md), so regenerating them wholesale would drop
# constraints. Closing that gap is tracked separately.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

CONTROLLER_GEN="${CONTROLLER_GEN:-controller-gen}"
if ! command -v "$CONTROLLER_GEN" >/dev/null 2>&1; then
  echo "error: controller-gen not found (set CONTROLLER_GEN or run 'make controller-gen')" >&2
  exit 1
fi

generated_dir="$(mktemp -d)"
trap 'rm -rf "$generated_dir"' EXIT

"$CONTROLLER_GEN" crd paths=./api/... "output:crd:artifacts:config=$generated_dir"

failed=0

# Invariant 1: every generated kind has a checked-in manifest.
shopt -s nullglob
for generated in "$generated_dir"/*.yaml; do
  # controller-gen emits <group>_<plural>.yaml; the repo stores <plural>.<group>.yaml
  base="$(basename "$generated" .yaml)"
  group="${base%%_*}"
  plural="${base#*_}"
  expected="k8s/crds/${plural}.${group}.yaml"

  if [[ ! -f "$expected" ]]; then
    echo "error: missing CRD manifest ${expected}" >&2
    echo "       api/ registers this kind but no manifest is checked in;" >&2
    echo "       the manager will fail its cache sync at startup." >&2
    echo "       generate it with: make manifests" >&2
    failed=1
  fi
done

# Invariant 2: the two manifest directories agree.
if ! diff -r k8s/crds helm/bindery-core/crds >/dev/null 2>&1; then
  echo "error: k8s/crds and helm/bindery-core/crds have diverged" >&2
  diff -r k8s/crds helm/bindery-core/crds >&2 || true
  failed=1
fi

if [[ "$failed" -ne 0 ]]; then
  exit 1
fi

echo "CRD manifests verified: every registered kind has a manifest, and both manifest directories agree."
