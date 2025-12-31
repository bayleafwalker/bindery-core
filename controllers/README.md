# /controllers

This folder contains Kubernetes controllers/reconcilers for Bindery.

Primary controller:
- CapabilityResolver reconciles `WorldInstance` inputs into `CapabilityBinding` outputs.

Key references:
- Controller manager entrypoint: `main.go`
- Controller implementation: `controllers/`
- Resolver logic used by the controller: `internal/resolver/`

Docs:
- Design spec: `docs/standards/kubernetes/capabilityresolver.md`
