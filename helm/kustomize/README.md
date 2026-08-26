# /deploy

This folder is intended for **deployment artifacts** (Kubernetes manifests, Helm/Kustomize overlays, dev scripts).

Current canonical sources in this repo:
- Operator CRDs: `k8s/crds/`, mirrored byte-for-byte into `helm/bindery-core/crds/`
- Operator chart: `helm/bindery-core/`
- External-runtime chart: `charts/bindery-external-runtime/` (deploys a standalone
  service; it ships no CRDs and is versioned separately on purpose)
- Example resources: `examples/booklet-bindery-sample/k8s/` — there is no
  `k8s/examples/` directory; the platform deliberately ships no game content
  under `k8s/`
- Local Kind demo scripts: `k8s/dev/`
- Controller config scaffolding (if used): `config/`

Notes:
- If you add/modify CRDs, keep `examples/booklet-bindery-sample/k8s/` and
  `docs/standards/kubernetes/crds.md` in sync, and run `make verify-crds` — it
  fails if `k8s/crds/` and `helm/bindery-core/crds/` diverge, or if a
  scheme-registered kind has no manifest.
