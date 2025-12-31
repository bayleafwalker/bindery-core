# /deploy

This folder is intended for **deployment artifacts** (Kubernetes manifests, Helm/Kustomize overlays, dev scripts).

Current canonical sources in this repo:
- CRDs: `k8s/crds/`
- Example resources: `k8s/examples/`
- Local Kind demo scripts: `k8s/dev/`
- Controller config scaffolding (if used): `config/`

Notes:
- If you add/modify CRDs, keep `k8s/examples/` and `docs/standards/kubernetes/crds.md` in sync.
