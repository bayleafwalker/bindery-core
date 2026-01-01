#!/usr/bin/env bash
set -euo pipefail

# Apply the sample game resources (assumes CRDs are already installed).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

cd "$REPO_ROOT"

# Cluster-scoped resources
kubectl apply -f examples/booklet-bindery-sample/k8s/01-capabilitydefinition-physics-engine.yaml

# Namespaced resources
kubectl apply -f examples/booklet-bindery-sample/k8s/00-namespace.yaml
kubectl apply -f examples/booklet-bindery-sample/k8s/10-modulemanifest-physics.yaml
kubectl apply -f examples/booklet-bindery-sample/k8s/12-modulemanifest-web.yaml
kubectl apply -f examples/booklet-bindery-sample/k8s/20-booklet.yaml
kubectl apply -f examples/booklet-bindery-sample/k8s/30-worldinstance.yaml

echo "Applied bindery-sample game resources."
