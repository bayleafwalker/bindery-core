# Runtime Coordination

This document describes how the Bindery platform coordinates the lifecycle and connectivity of game modules at runtime.

## Overview

Runtime coordination ensures that:
1.  All required modules for a World Instance are deployed and running.
2.  Modules can discover and connect to their dependencies.
3.  Startup order is managed to prevent transient failures.
4.  Global (cluster-scoped) services are supported alongside world-scoped modules.

## Core Concepts

### Root Modules
A "Root Module" is a module listed in a `Booklet` that is not required by any other module (e.g., a Game Server or Gateway).
The `CapabilityResolver` automatically detects these modules and creates a **Root CapabilityBinding** to ensure they are deployed.
- **Consumer**: The World Instance itself (synthetic).
- **Provider**: The Root Module.
- **CapabilityID**: `system.root` (see `internal/resolver/default_resolver.go`).

`RealmReconciler` emits the same synthetic capability for realm modules, at
`scope: realm` rather than the world scope.

### Global Services
Scope values are lowercase and come from the `CapabilityScope` enum in
`api/v1alpha1/common_types.go`: `cluster`, `region`, `realm`, `world`,
`world-shard`, `session`.

- **Binding**: The `CapabilityResolver` binds cluster- and region-scoped
  requirements; `RealmReconciler` separately creates one `system.root` binding
  at `scope: realm` per module listed in `Realm.spec.modules`.
- **Orchestration**: The `RuntimeOrchestrator` deploys a single instance of the provider, shared across all consumers.
- **Isolation**: Global services do not belong to a specific World Instance and are not subject to world-specific logic (like `WorldStorageClaim`).

### Readiness Coordination
To ensure smooth startup, the platform injects **Init Containers** into module deployments.
- **Mechanism**: The init container waits for the Service DNS of all dependencies to be resolvable.
- **Benefit**: Prevents application crash loops caused by missing dependencies.

## Observability

Registered in `controllers/metrics.go`:

- `bindery_controller_reconcile_total`, `bindery_controller_reconcile_error_total`
- `bindery_capabilityresolver_unresolved_required`
- `bindery_capabilityresolver_bindings_created_total`,
  `bindery_capabilityresolver_bindings_updated_total`,
  `bindery_capabilityresolver_bindings_deleted_total`
- `bindery_capabilityresolver_resolution_duration_seconds`: Histogram of resolution time.
- `bindery_runtimeorchestrator_deployment_duration_seconds`: Histogram of deployment reconciliation time.

`make run-controller` disables the metrics listener; use
`make run-controller-with-metrics` to expose them on `:8080`.

### CLI
`kubectl get capabilitybindings` now shows:
- **Scope**: World, Cluster, etc.
- **Provider**: The module providing the capability.
- **Consumer**: The module consuming the capability.
- **World**: The owning world (if applicable).
