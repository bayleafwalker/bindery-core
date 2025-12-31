# Production Readiness & Scalability Guide

This guide outlines best practices for running Bindery in production environments, addressing scalability, performance, and operational limits.

## Kubernetes Limitations & Scalability

Bindery relies heavily on Kubernetes CRDs (`WorldInstance`, `CapabilityBinding`, etc.). While Kubernetes is robust, it has limits:

*   **Object Count**: A single cluster can handle thousands of CRs, but tens of thousands may strain `etcd` and the API server.
*   **Scheduling Latency**: Pod startup is not instantaneous. Expect seconds of latency for new world creation.
*   **Controller Throughput**: High churn rates (rapid world creation/deletion) can backlog controller queues.

### Recommendations

1.  **Multi-Cluster Architecture**: For large-scale games (>5k concurrent worlds), shard your deployment across multiple Kubernetes clusters (e.g., by region or game mode).
2.  **Warm Pools**: To mitigate scheduling latency, maintain a pool of "warm" worlds or pre-provisioned nodes.
3.  **Etcd Tuning**: Ensure your `etcd` cluster is backed by fast SSDs and has sufficient CPU/memory.

## Resource Management

### Controller Resources

Ensure the Bindery controller manager has sufficient resources. Recommended defaults for a moderate cluster:

```yaml
resources:
  requests:
    cpu: 500m
    memory: 512Mi
  limits:
    cpu: 2000m
    memory: 2Gi
```

Monitor CPU usage during peak load and adjust accordingly.

### Priority Classes

Use Kubernetes `PriorityClasses` to ensure critical game servers take precedence over background tasks.

1.  Define a PriorityClass:
    ```yaml
    apiVersion: scheduling.k8s.io/v1
    kind: PriorityClass
    metadata:
      name: high-priority-game
    value: 1000000
    globalDefault: false
    description: "This priority class should be used for game server pods."
    ```

2.  Reference it in your `ModuleManifest`:
    ```yaml
    spec:
      scheduling:
        priorityClassName: high-priority-game
    ```

## Networking & Node Affinity

To minimize latency:

*   **Node Affinity**: Use `nodeSelector` or `affinity` in `ModuleManifest` to schedule game servers on optimized node pools.
*   **Colocation**: Use `Booklet` colocation strategies to keep related modules together.

## Monitoring

Key metrics to watch:

*   **World Startup Latency**: Time from `WorldInstance` creation to `Running` phase.
*   **Reconciliation Rate**: Rate of successful vs. failed reconciliations in the controller logs.
*   **API Server Latency**: High latency indicates `etcd` stress or controller overload.

## Load Testing

Use the `bindery-load-test` tool to benchmark your cluster's performance:

```bash
go run ./cmd/bindery-load-test --worlds 100 --namespace default
```
