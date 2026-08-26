# /controllers

This folder contains Kubernetes controllers/reconcilers for Bindery.

`main.go` registers six reconcilers, unconditionally:

- `CapabilityResolverReconciler` reconciles `WorldInstance` inputs into
  `CapabilityBinding` outputs.
- `RuntimeOrchestratorReconciler` materializes `Deployment`/`Service` for
  server-owned provider modules and publishes the endpoint back onto
  `CapabilityBinding.status.provider.endpoint`.
- `WorldShardReconciler` creates/removes `WorldShard` objects to match
  `WorldInstance.spec.shardCount`.
- `StorageOrchestratorReconciler` reconciles `WorldStorageClaim` into a PVC or,
  for the client low-latency tier, an external URI in status.
- `RealmReconciler` ensures a `CapabilityBinding` exists per module in
  `Realm.spec`.
- `ShardAutoscalerReconciler` adjusts `WorldInstance.spec.shardCount` from
  metrics, clamped by `minShards`/`maxShards`.

Because registration is unconditional, every registered kind needs an installed
CRD or the manager exits on a cache-sync timeout. `make verify-crds` guards
that.

The external runtime, the other subsystem in this repository, has no
controllers. See `docs/README.md`.

Key references:
- Controller manager entrypoint: `main.go`
- Controller implementation: `controllers/`
- Resolver logic used by the controller: `internal/resolver/`

Docs:
- Resolver design spec: `docs/standards/kubernetes/capabilityresolver.md`
- CRDs and the generation/drift rules: `docs/standards/kubernetes/crds.md`
- Realm hierarchy: `docs/standards/realm-architecture.md`
- Sharding and autoscaler clamp semantics: `docs/standards/shard-autoscaling.md`
- Troubleshooting: `docs/debugging/runtime-coordination.md`

Metrics emitted from `metrics.go`:
`bindery_controller_reconcile_total`,
`bindery_controller_reconcile_error_total`,
`bindery_capabilityresolver_unresolved_required`,
`bindery_capabilityresolver_bindings_{created,updated,deleted}_total`,
`bindery_capabilityresolver_resolution_duration_seconds`,
`bindery_runtimeorchestrator_deployment_duration_seconds`.
