# Shard Autoscaling Standard (v1alpha1)

This document defines the standard for dynamic sharding of World Instances in Bindery.

## Overview

Bindery supports partitioning a World Instance into multiple "shards" to handle scale. Each shard is an isolated slice of the world (e.g., a spatial partition or a load-balanced bucket) that runs its own set of module providers.

The `ShardAutoscaler` resource allows operators to define policies for automatically adjusting the number of shards (`WorldInstance.spec.shardCount`) based on real-time metrics.

## The `ShardAutoscaler` Resource

The `ShardAutoscaler` is a namespaced Custom Resource that targets a specific `WorldInstance`.

### Schema

```yaml
apiVersion: bindery.platform/v1alpha1
kind: ShardAutoscaler
metadata:
  name: my-world-scaler
  namespace: default
spec:
  # Reference to the WorldInstance to scale
  worldRef:
    name: my-world

  # Bounds
  minShards: 1
  maxShards: 10

  # Scaling Policy
  metrics:
    - type: Resource
      resource:
        name: cpu  # or "memory"
        targetAverageUtilization: 70  # Target 70% utilization
```

### Behavior

1.  **Metric Collection**: The controller queries the Kubernetes Metrics API for all Pods belonging to the target World.
2.  **Aggregation**: It calculates the average utilization (CPU or Memory) across all pods in the world.
3.  **Calculation**:
    *   `desiredShards = currentShards * (currentUtilization / targetUtilization)`
    *   The result is clamped between `minShards` and `maxShards`.
4.  **Actuation**: If `desiredShards` differs from `currentShards`, the controller updates `WorldInstance.spec.shardCount`.
5.  **Reconciliation**: The `WorldShardController` sees the updated count and creates/deletes `WorldShard` CRs. The `CapabilityResolver` and `RuntimeOrchestrator` then react to provision/deprovision infrastructure.

## Graceful Scale-Down

Scaling down is **destructive**: it removes the highest-indexed shards (e.g., scaling from 5 to 4 deletes Shard 4).

To ensure player experience is preserved, modules running in sharded environments **MUST** handle termination gracefully.

### Module Requirements

1.  **Handle SIGTERM**: When a shard is deleted, its pods receive `SIGTERM`. The application should stop accepting new requests and flush state.
2.  **PreStop Hooks**: Use `ModuleManifest.spec.runtime.preStopCommand` (or the legacy `bindery.dev/pre-stop-command` annotation) to run a script before the process receives `SIGTERM`.
3.  **Grace Period**: Use `ModuleManifest.spec.runtime.terminationGracePeriodSeconds` (or the legacy `bindery.dev/termination-grace-period` annotation) to request sufficient time (e.g., 60s) for draining.

### Example Manifest

```yaml
apiVersion: bindery.platform/v1alpha1
kind: ModuleManifest
metadata:
  name: physics-engine
spec:
  runtime:
    image: "physics:v2"
    terminationGracePeriodSeconds: 60
    preStopCommand: "/bin/drain.sh"
  # ...
```
