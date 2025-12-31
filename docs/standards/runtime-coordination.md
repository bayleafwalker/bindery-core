# Runtime Coordination

This document describes how the Anvil platform coordinates the lifecycle and connectivity of game modules at runtime.

## Overview

Runtime coordination ensures that:
1.  All required modules for a World Instance are deployed and running.
2.  Modules can discover and connect to their dependencies.
3.  Startup order is managed to prevent transient failures.
4.  Global (cluster-scoped) services are supported alongside world-scoped modules.

## Core Concepts

### Root Modules
A "Root Module" is a module listed in a `GameDefinition` that is not required by any other module (e.g., a Game Server or Gateway).
The `CapabilityResolver` automatically detects these modules and creates a **Root CapabilityBinding** to ensure they are deployed.
- **Consumer**: The World Instance itself (synthetic).
- **Provider**: The Root Module.
- **CapabilityID**: `root`.

### Global Services
Modules can be scoped to `Cluster` or `Region`.
- **Binding**: The `CapabilityResolver` or `RealmController` creates bindings with `scope: cluster`.
- **Orchestration**: The `RuntimeOrchestrator` deploys a single instance of the provider, shared across all consumers.
- **Isolation**: Global services do not belong to a specific World Instance and are not subject to world-specific logic (like `WorldStorageClaim`).

### Readiness Coordination
To ensure smooth startup, the platform injects **Init Containers** into module deployments.
- **Mechanism**: The init container waits for the Service DNS of all dependencies to be resolvable.
- **Benefit**: Prevents application crash loops caused by missing dependencies.

## Observability

### Metrics
- `anvil_capabilityresolver_resolution_duration_seconds`: Histogram of resolution time.
- `anvil_runtimeorchestrator_deployment_duration_seconds`: Histogram of deployment reconciliation time.

### CLI
`kubectl get capabilitybindings` now shows:
- **Scope**: World, Cluster, etc.
- **Provider**: The module providing the capability.
- **Consumer**: The module consuming the capability.
- **World**: The owning world (if applicable).
