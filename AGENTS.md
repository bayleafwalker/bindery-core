# Bindery Core Agent Guidance

> Shared environment guidance lives in `/projects/dev/AGENTS.md`.

Bindery Core is an experimental, pre-alpha Go and Kubernetes-native game
platform. Its `v1alpha1` CRDs, controller behavior, Helm resources, and gRPC
contracts can change; do not describe the project as production-ready.

- Keep API definitions, controllers, generated CRDs, Helm resources, and
  contract documentation aligned when changing a capability or lifecycle
  boundary.
- Validate ordinary Go changes with:
  ```bash
  make verify
  ```
  This target runs formatting and dependency tidying as well as tests, so review
  any resulting module or source changes.
- Use `make test-integration` only when envtest setup is acceptable. Use
  `make test-e2e`, `make kind-demo`, `make kind-down`, and controller runs only
  with an explicitly verified local Kubernetes context.
- Do not apply Helm manifests, mutate a shared cluster, or treat sample game
  assets as a supported production deployment without separate authority.

Read the relevant standards and contract documents before changing semantic
versioning, capability resolution, storage binding, or RPC behavior.