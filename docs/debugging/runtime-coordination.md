# Debugging Runtime Coordination

This guide helps you troubleshoot issues where modules are not starting, connecting, or behaving as expected.

## 1. Check the Bindings
The `CapabilityBinding` is the source of truth for what should be running.

```bash
kubectl get capabilitybindings -l game.platform/world=<world-name>
```

Look for:
- **Root Bindings**: `root` capability. These ensure entry-point modules start.
- **Global Bindings**: `Scope=realm`. These point to shared services.
- **Status**: Should be `Bound` or `Ready`.

## 2. Trace the Flow

### Scenario: "My Game Server isn't starting"
1.  **Check Booklet**: Is the module listed?
2.  **Check CapabilityResolver**:
    - Did it create a binding?
    - If not, check `WorldInstance` status for `ModuleManifestNotFound` or resolution errors.
    - `kubectl describe world <world-name>`
3.  **Check RuntimeOrchestrator**:
    - If binding exists, is there a Deployment?
    - `kubectl get deployment -l game.platform/module=<module-name>`
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
Prometheus metrics available:
- `bindery_capabilityresolver_resolution_duration_seconds`
- `bindery_runtimeorchestrator_deployment_duration_seconds`
