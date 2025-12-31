#!/usr/bin/env bash
set -euo pipefail

# Applies CRDs and example resources.
# Assumes your kubectl context points at the target cluster.

kubectl apply -f k8s/crds/

# Cluster-scoped resources
kubectl apply -f k8s/examples/01-capabilitydefinition-physics-engine.yaml
kubectl apply -f k8s/examples/02-capabilitydefinition-interaction-engine.yaml

# Namespaced resources
kubectl apply -f k8s/examples/00-namespace.yaml
kubectl apply -f k8s/examples/game-dev/10-modulemanifest-physics.yaml
kubectl apply -f k8s/examples/game-dev/11-modulemanifest-interaction.yaml
kubectl apply -f k8s/examples/game-dev/20-booklet.yaml
kubectl apply -f k8s/examples/game-dev/30-worldinstance.yaml

echo "Applied CRDs + examples."
