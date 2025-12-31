# Realm Architecture

The Anvil platform uses a hierarchical model to organize game worlds and shared services. This document describes the `Realm` concept and how it interacts with Worlds and the Runtime.

## Hierarchy

```mermaid
graph TD
    Realm[Realm (Region/Cluster)] -->|Contains| GlobalModules[Global Modules]
    Realm -->|Groups| World[WorldInstance]
    World -->|Contains| WorldModules[World Modules]
    World -->|Sharded into| Shard[WorldShard]
    
    GlobalModules -.->|Provided to| WorldModules
```

### 1. Realm
A **Realm** represents a shared context for a group of World Instances. It typically maps to a geographic region (e.g., `eu-west`, `us-east`) or a logical cluster.
- **Responsibility**: Hosting "Global" services that are shared across multiple worlds (e.g., Matchmaking, Chat, Player Inventory).
- **Resource**: `Realm` CRD.
- **Controller**: `RealmController`.

### 2. WorldInstance
A **WorldInstance** is a specific instantiation of a `GameDefinition`.
- **Responsibility**: Hosting game logic, physics, and world-specific state.
- **Resource**: `WorldInstance` CRD.
- **Controller**: `CapabilityResolver` (logic), `WorldShardController` (sharding).

### 3. Runtime (Orchestration)
The **Runtime** layer is responsible for materializing the abstract modules into Kubernetes workloads (Deployments, Services).
- **Responsibility**: Ensuring code is running and reachable.
- **Resource**: `CapabilityBinding` (the bridge between logic and runtime).
- **Controller**: `RuntimeOrchestrator`.

## Cross-Scope Coordination

Modules in a World often need to communicate with Global modules in the Realm.

1.  **Declaration**: A World module declares a requirement with `scope: realm`.
    ```yaml
    requires:
      - capabilityId: "chat"
        scope: "realm"
    ```
2.  **Resolution**: The `CapabilityResolver` looks up the `Realm` associated with the World (via `spec.realmRef`) and finds a matching module.
3.  **Binding**: A `CapabilityBinding` is created linking the World module (Consumer) to the Realm module (Provider).
4.  **Deployment**:
    - The `RealmController` ensures the Global Module is running (via a "Root" binding).
    - The `RuntimeOrchestrator` ensures the World Module is running and injects the connection details (Service DNS) of the Global Module.

## Service Discovery
The platform automatically injects environment variables for service discovery:
- `ANVIL_CAPABILITY_<ID>_ENDPOINT`: `host:port`
- `ANVIL_CAPABILITY_<ID>_HOST`: `host`
- `ANVIL_CAPABILITY_<ID>_PORT`: `port`

For Realm-scoped dependencies, these point to the single shared Service of the global module.
