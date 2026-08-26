# Debugging Runtime Coordination

This guide helps you troubleshoot issues where modules are not starting, connecting, or behaving as expected.

Scope: the **Kubernetes operator** only. The external runtime in this repository
has no CRDs and no controllers; nothing here applies to it.

## 1. Check the Bindings
The `CapabilityBinding` is the source of truth for what should be running.

```bash
kubectl get capabilitybindings -l bindery.platform/world=<world-name>
```

Look for:
- **Root Bindings**: the synthetic `system.root` capability. These ensure
  entry-point modules start.
- **Global Bindings**: `Scope=realm`. These point to shared services.
- **Status**: the `CapabilityBinding` CRD prints a `Phase` column from
  `.status.phase`, but **no controller writes that field** — it will be empty.
  The signals that are actually written are the `RuntimeReady` condition in
  `.status.conditions` and the resolved endpoint in
  `.status.provider.endpoint`:

  ```bash
  kubectl get capabilitybinding <name> -o jsonpath='{.status.provider.endpoint}{"\n"}'
  kubectl get capabilitybinding <name> \
    -o jsonpath='{range .status.conditions[?(@.type=="RuntimeReady")]}{.status} {.reason}{"\n"}{end}'
  ```

## 2. Trace the Flow

### Scenario: "My Game Server isn't starting"
1.  **Check Booklet**: Is the module listed?
2.  **Check CapabilityResolver**:
    - Did it create a binding?
    - If not, check `WorldInstance` status for `ModuleManifestNotFound` or resolution errors.
    - `kubectl describe world <world-name>`
3.  **Check RuntimeOrchestrator**:
    - If binding exists, is there a Deployment?
    - `kubectl get deployment -l bindery.platform/module=<module-name>`
    - If no deployment, check `RuntimeOrchestrator` logs. Does the module have `bindery.dev/runtime-image` annotation?

### Scenario: "My module crashes on startup"
1.  **Check Init Containers**:
    - We inject a `wait-for-deps` init container.
    - If the pod is stuck in `Init:0/1`, it means a dependency is not reachable.
    - Check logs: `kubectl logs <pod> -c wait-for-deps`
2.  **Check Dependency Services**:
    - Are the Services for the required modules up?
    - `kubectl get svc`

## 3. Realm/Global Issues
If a global service is missing:
1.  Check the `Realm` resource.
2.  Check `RealmController` logs.
3.  Verify `CapabilityBinding` with `Scope=realm` exists.

## 4. Metrics
Registered in `controllers/metrics.go`:
- `bindery_controller_reconcile_total`, `bindery_controller_reconcile_error_total`
- `bindery_capabilityresolver_unresolved_required`
- `bindery_capabilityresolver_bindings_created_total`,
  `bindery_capabilityresolver_bindings_updated_total`,
  `bindery_capabilityresolver_bindings_deleted_total`
- `bindery_capabilityresolver_resolution_duration_seconds`
- `bindery_runtimeorchestrator_deployment_duration_seconds`

`make run-controller` disables the metrics listener; use
`make run-controller-with-metrics` to expose `:8080`.

## 5. Sharding

If a world will not scale, or will not scale back, read the clamp semantics in
[`../standards/shard-autoscaling.md`](../standards/shard-autoscaling.md) first:
`minShards` only raises and `maxShards` only lowers, and `status.currentShards`
lags one reconcile pass. Lowering `minShards` alone will not shrink a world that
has already grown.
