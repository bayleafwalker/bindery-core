# Kubernetes CRDs (v1alpha1)

This document describes the Kubernetes Custom Resource Definitions (CRDs) that formalize the platform’s declarative model as Kubernetes resources.

All CRDs:
- use `apiextensions.k8s.io/v1`
- include OpenAPI v3 validation
- use camelCase field names in `spec` and `status`
- are intended to be applied with `kubectl apply -f k8s/crds/`

## Validation (verified with Kind)

`kubectl apply` performs API discovery and OpenAPI validation against a live API server.

These CRDs and the example resources under `k8s/examples/` have been validated by applying them to a local Kind cluster (kind v0.23.0).

Recommended validation options:

1) Local ephemeral cluster (fastest):

```bash
./k8s/dev/kind-demo.sh anvil-crds
```

2) Real cluster later:

```bash
kubectl apply -f k8s/crds/
```

If your `kubectl` is currently pointed at a non-existent localhost API server, you’ll see connection errors until you set up a cluster or update kubeconfig.

Note: The authoritative CRD YAML is in `k8s/crds/`. This document includes inlined YAML for readability, but you should apply the files from `k8s/crds/`.

## Example resources

Once the CRDs are installed, example custom resources live under `k8s/examples/`.

Suggested apply order:

```bash
kubectl apply -f k8s/crds/
kubectl apply -f k8s/examples/00-namespace.yaml
kubectl apply -f k8s/examples/01-capabilitydefinition-physics-engine.yaml
kubectl apply -f k8s/examples/02-capabilitydefinition-interaction-engine.yaml
kubectl apply -f k8s/examples/game-dev/
```

## 1) ModuleManifest CRD

**Purpose:** Declares a module’s identity and its `provides[]` / `requires[]` capability contracts, plus scaling hints.

**File:** `k8s/crds/modulemanifests.game.platform.yaml`

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: modulemanifests.game.platform
spec:
  group: game.platform
  scope: Namespaced
  names:
    plural: modulemanifests
    singular: modulemanifest
    kind: ModuleManifest
    shortNames:
      - mm
  versions:
    - name: v1alpha1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          description: Declarative module contract describing provided and required capabilities.
          required:
            - spec
          properties:
            apiVersion:
              type: string
            kind:
              type: string
            metadata:
              type: object
            spec:
              type: object
              required:
                - module
                - provides
                - requires
                - scaling
              properties:
                module:
                  type: object
                  required:
                    - id
                    - version
                  properties:
                    id:
                      type: string
                      minLength: 1
                      description: Globally unique module identifier (e.g., core.physics).
                    version:
                      type: string
                      pattern: "^(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?(?:\\+[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?$"
                      description: Module artifact semantic version.
                    description:
                      type: string
                    owners:
                      type: array
                      items:
                        type: string
                    repo:
                      type: string
                    license:
                      type: string
                provides:
                  type: array
                  description: Capabilities provided by this module.
                  items:
                    type: object
                    required:
                      - capabilityId
                      - version
                      - scope
                      - multiplicity
                    properties:
                      capabilityId:
                        type: string
                        pattern: "^[a-z][a-z0-9-]*(\\.[a-z][a-z0-9-]*)+$"
                      version:
                        type: string
                        pattern: "^(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?(?:\\+[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?$"
                      scope:
                        type: string
                        enum: [cluster, region, world, world-shard, session]
                      multiplicity:
                        type: string
                        enum: ["1", many]
                      features:
                        type: object
                        properties:
                          supported:
                            type: array
                            items:
                              type: string
                      nfr:
                        type: object
                        properties:
                          latency:
                            type: object
                            properties:
                              p95Ms:
                                type: object
                                required: [value, constraint]
                                properties:
                                  value:
                                    type: number
                                    minimum: 0
                                  constraint:
                                    type: string
                                    enum: [hard, soft]
                          tickRateHz:
                            type: object
                            required: [value, constraint]
                            properties:
                              value:
                                type: number
                                minimum: 0
                              constraint:
                                type: string
                                enum: [hard, soft]
                          determinism:
                            type: object
                            required: [value, constraint]
                            properties:
                              value:
                                type: string
                                enum: [required, best-effort, none]
                              constraint:
                                type: string
                                enum: [hard, soft]
                      interfaces:
                        type: object
                        properties:
                          grpc:
                            type: array
                            items:
                              type: object
                              required: [package, service, protoRef]
                              properties:
                                package:
                                  type: string
                                service:
                                  type: string
                                protoRef:
                                  type: string
                                methods:
                                  type: array
                                  items:
                                    type: object
                                    required: [name, request, response]
                                    properties:
                                      name:
                                        type: string
                                      request:
                                        type: string
                                      response:
                                        type: string
                          events:
                            type: array
                            items:
                              type: object
                              required: [name, direction, schema]
                              properties:
                                name:
                                  type: string
                                direction:
                                  type: string
                                  enum: [publish, subscribe, both]
                                schema:
                                  type: object
                                  required: [id, version, format, schemaRef]
                                  properties:
                                    id:
                                      type: string
                                    version:
                                      type: string
                                      pattern: "^(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?(?:\\+[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?$"
                                    format:
                                      type: string
                                      enum: [protobuf, json, avro]
                                    schemaRef:
                                      type: string
                                orderingKey:
                                  type: string
                requires:
                  type: array
                  description: Capabilities required by this module.
                  items:
                    type: object
                    required:
                      - capabilityId
                      - versionConstraint
                      - scope
                      - multiplicity
                      - dependencyMode
                    properties:
                      capabilityId:
                        type: string
                        pattern: "^[a-z][a-z0-9-]*(\\.[a-z][a-z0-9-]*)+$"
                      versionConstraint:
                        type: string
                        minLength: 1
                        description: SemVer range expression (e.g., ^1.2.0).
                      scope:
                        type: string
                        enum: [cluster, region, world, world-shard, session]
                      multiplicity:
                        type: string
                        enum: ["1", many]
                      dependencyMode:
                        type: string
                        enum: [required, optional]
                      features:
                        type: object
                        properties:
                          required:
                            type: array
                            items:
                              type: string
                          preferred:
                            type: array
                            items:
                              type: string
                      nfr:
                        type: object
                        properties:
                          latency:
                            type: object
                            properties:
                              p95Ms:
                                type: object
                                required: [value, constraint]
                                properties:
                                  value:
                                    type: number
                                    minimum: 0
                                  constraint:
                                    type: string
                                    enum: [hard, soft]
                          tickRateHz:
                            type: object
                            required: [value, constraint]
                            properties:
                              value:
                                type: number
                                minimum: 0
                              constraint:
                                type: string
                                enum: [hard, soft]
                scaling:
                  type: object
                  required: [defaultScope, statefulness]
                  properties:
                    defaultScope:
                      type: string
                      enum: [cluster, region, world, world-shard, session]
                    statefulness:
                      type: string
                      enum: [stateless, stateful]
                    sharding:
                      type: object
                      properties:
                        strategy:
                          type: string
                          enum: [none, world, world-shard, session, custom]
                        key:
                          type: string
                    autoscaling:
                      type: object
                      properties:
                        minReplicas:
                          type: integer
                          minimum: 0
                        maxReplicas:
                          type: integer
                          minimum: 0
                        metricHints:
                          type: array
                          items:
                            type: object
                            required: [type, target]
                            properties:
                              type:
                                type: string
                                enum: [cpu, memory, rps, tick-lag, custom]
                              target:
                                type: string
            status:
              type: object
              properties:
                observedGeneration:
                  type: integer
                  minimum: 0
                phase:
                  type: string
                  enum: [Pending, Resolved, Error]
                message:
                  type: string
                unresolvedRequires:
                  type: array
                  items:
                    type: object
                    required: [capabilityId, reason]
                    properties:
                      capabilityId:
                        type: string
                      reason:
                        type: string
                conditions:
                  type: array
                  items:
                    type: object
                    required: [type, status]
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                        enum: ["True", "False", "Unknown"]
                      reason:
                        type: string
                      message:
                        type: string
                      lastTransitionTime:
                        type: string
                        format: date-time
          x-kubernetes-validations:
            - rule: "has(self.spec.provides)"
              message: "spec.provides must be present"
            - rule: "has(self.spec.requires)"
              message: "spec.requires must be present"
```

## 2) CapabilityDefinition CRD

**Purpose:** Cluster-wide registry entry for a capability family (capability ID) and its known versions/scopes/features.

**File:** `k8s/crds/capabilitydefinitions.game.platform.yaml`

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: capabilitydefinitions.game.platform
spec:
  group: game.platform
  scope: Cluster
  names:
    plural: capabilitydefinitions
    singular: capabilitydefinition
    kind: CapabilityDefinition
    shortNames:
      - capdef
  versions:
    - name: v1alpha1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          description: Defines a capability contract family and available versions for discovery and policy.
          required: [spec]
          properties:
            apiVersion:
              type: string
            kind:
              type: string
            metadata:
              type: object
            spec:
              type: object
              required:
                - capabilityId
                - latestVersion
                - scopes
                - multiplicity
              properties:
                capabilityId:
                  type: string
                  pattern: "^[a-z][a-z0-9-]*(\\.[a-z][a-z0-9-]*)+$"
                  description: Namespaced capability ID (e.g., physics.engine).
                latestVersion:
                  type: string
                  pattern: "^(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?(?:\\+[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?$"
                versions:
                  type: array
                  description: Optional list of known published versions.
                  items:
                    type: string
                    pattern: "^(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?(?:\\+[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?$"
                scopes:
                  type: array
                  minItems: 1
                  items:
                    type: string
                    enum: [cluster, region, world, world-shard, session]
                multiplicity:
                  type: string
                  enum: ["1", many]
                features:
                  type: object
                  properties:
                    defined:
                      type: array
                      items:
                        type: object
                        required: [name, stability]
                        properties:
                          name:
                            type: string
                            minLength: 1
                          stability:
                            type: string
                            enum: [experimental, stable, deprecated]
                          description:
                            type: string
                contractRef:
                  type: object
                  description: Optional pointer to a CapabilityContract resource/doc registry.
                  properties:
                    kind:
                      type: string
                      enum: [CapabilityContract]
                    name:
                      type: string
                nfrDefaults:
                  type: object
                  properties:
                    tickRateHz:
                      type: number
                      minimum: 0
                    latencyP95Ms:
                      type: number
                      minimum: 0
                    determinism:
                      type: string
                      enum: [required, best-effort, none]
            status:
              type: object
              properties:
                observedGeneration:
                  type: integer
                  minimum: 0
                phase:
                  type: string
                  enum: [Active, Deprecated, Retired]
                message:
                  type: string
                conditions:
                  type: array
                  items:
                    type: object
                    required: [type, status]
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                        enum: ["True", "False", "Unknown"]
                      reason:
                        type: string
                      message:
                        type: string
                      lastTransitionTime:
                        type: string
                        format: date-time
```

## 3) GameDefinition CRD

**Purpose:** Declares a game configuration composed of modules, plus environment defaults (region/shards/tick rate).

**File:** `k8s/crds/gamedefinitions.game.platform.yaml`

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: gamedefinitions.game.platform
spec:
  group: game.platform
  scope: Namespaced
  names:
    plural: gamedefinitions
    singular: gamedefinition
    kind: GameDefinition
    shortNames:
      - game
  versions:
    - name: v1alpha1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          description: Declares a game composed of modules and their intended capability topology.
          required: [spec]
          properties:
            apiVersion:
              type: string
            kind:
              type: string
            metadata:
              type: object
            spec:
              type: object
              required:
                - gameId
                - version
                - modules
              properties:
                gameId:
                  type: string
                  minLength: 1
                  description: Stable game identifier.
                version:
                  type: string
                  pattern: "^(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?(?:\\+[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?$"
                  description: Game definition semantic version.
                description:
                  type: string
                modules:
                  type: array
                  minItems: 1
                  description: Set of modules that constitute this game.
                  items:
                    type: object
                    required: [name]
                    properties:
                      name:
                        type: string
                        minLength: 1
                        description: Name of a ModuleManifest in the same namespace.
                      required:
                        type: boolean
                        default: true
                        description: Whether the module is required for this game to be valid.
                      desiredScope:
                        type: string
                        enum: [cluster, region, world, world-shard, session]
                      parameters:
                        type: object
                        additionalProperties:
                          type: string
                defaults:
                  type: object
                  properties:
                    region:
                      type: string
                    shardCount:
                      type: integer
                      minimum: 1
                    tickRateHz:
                      type: number
                      minimum: 0
            status:
              type: object
              properties:
                observedGeneration:
                  type: integer
                  minimum: 0
                phase:
                  type: string
                  enum: [Pending, Valid, Error]
                message:
                  type: string
                resolvedModules:
                  type: array
                  items:
                    type: object
                    required: [name, status]
                    properties:
                      name:
                        type: string
                      status:
                        type: string
                        enum: [Found, Missing, Invalid]
                      reason:
                        type: string
                conditions:
                  type: array
                  items:
                    type: object
                    required: [type, status]
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                        enum: ["True", "False", "Unknown"]
                      reason:
                        type: string
                      message:
                        type: string
                      lastTransitionTime:
                        type: string
                        format: date-time
```

## 4) WorldInstance CRD

**Purpose:** Declares a concrete world instance created from a GameDefinition (including shard count and desired state).

**File:** `k8s/crds/worldinstances.game.platform.yaml`

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: worldinstances.game.platform
spec:
  group: game.platform
  scope: Namespaced
  names:
    plural: worldinstances
    singular: worldinstance
    kind: WorldInstance
    shortNames:
      - world
  versions:
    - name: v1alpha1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          description: An instantiated world (or realm) created from a GameDefinition.
          required: [spec]
          properties:
            apiVersion:
              type: string
            kind:
              type: string
            metadata:
              type: object
            spec:
              type: object
              required:
                - gameRef
                - worldId
                - region
                - shardCount
              properties:
                gameRef:
                  type: object
                  required: [name]
                  properties:
                    name:
                      type: string
                      minLength: 1
                      description: Name of a GameDefinition in the same namespace.
                worldId:
                  type: string
                  minLength: 1
                  description: Stable identifier for this world instance.
                region:
                  type: string
                  minLength: 1
                shardCount:
                  type: integer
                  minimum: 1
                desiredState:
                  type: string
                  enum: [Running, Stopped]
                  default: Running
                parameters:
                  type: object
                  additionalProperties:
                    type: string
            status:
              type: object
              properties:
                observedGeneration:
                  type: integer
                  minimum: 0
                phase:
                  type: string
                  enum: [Pending, Provisioning, Running, Stopped, Error]
                message:
                  type: string
                shardStatuses:
                  type: array
                  items:
                    type: object
                    required: [shardId, phase]
                    properties:
                      shardId:
                        type: string
                      phase:
                        type: string
                        enum: [Pending, Running, Error]
                      endpoints:
                        type: array
                        items:
                          type: string
                conditions:
                  type: array
                  items:
                    type: object
                    required: [type, status]
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                        enum: ["True", "False", "Unknown"]
                      reason:
                        type: string
                      message:
                        type: string
                      lastTransitionTime:
                        type: string
                        format: date-time
```

## 5) CapabilityBinding CRD

**Purpose:** Declares an explicit binding from a consumer requirement to a provider (optionally scoped to a world).

**File:** `k8s/crds/capabilitybindings.game.platform.yaml`

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: capabilitybindings.game.platform
spec:
  group: game.platform
  scope: Namespaced
  names:
    plural: capabilitybindings
    singular: capabilitybinding
    kind: CapabilityBinding
    shortNames:
      - capbind
  versions:
    - name: v1alpha1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          description: Declares an explicit binding from a consumer requirement to a provider capability (optionally within a world instance).
          required: [spec]
          properties:
            apiVersion:
              type: string
            kind:
              type: string
            metadata:
              type: object
            spec:
              type: object
              required:
                - capabilityId
                - scope
                - multiplicity
                - consumer
                - provider
              properties:
                capabilityId:
                  type: string
                  pattern: "^[a-z][a-z0-9-]*(\\.[a-z][a-z0-9-]*)+$"
                scope:
                  type: string
                  enum: [cluster, region, world, world-shard, session]
                multiplicity:
                  type: string
                  enum: ["1", many]
                worldRef:
                  type: object
                  properties:
                    name:
                      type: string
                      minLength: 1
                      description: Optional WorldInstance name to scope this binding.
                consumer:
                  type: object
                  required: [moduleManifestName]
                  properties:
                    moduleManifestName:
                      type: string
                      minLength: 1
                    requirement:
                      type: object
                      properties:
                        versionConstraint:
                          type: string
                          minLength: 1
                        dependencyMode:
                          type: string
                          enum: [required, optional]
                        requiredFeatures:
                          type: array
                          items:
                            type: string
                        preferredFeatures:
                          type: array
                          items:
                            type: string
                provider:
                  type: object
                  required: [moduleManifestName]
                  properties:
                    moduleManifestName:
                      type: string
                      minLength: 1
                    capabilityVersion:
                      type: string
                      pattern: "^(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?(?:\\+[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?$"
                    endpoint:
                      type: object
                      properties:
                        type:
                          type: string
                          enum: [kubernetesService, url]
                        value:
                          type: string
                        port:
                          type: integer
                          minimum: 1
                          maximum: 65535
            status:
              type: object
              properties:
                observedGeneration:
                  type: integer
                  minimum: 0
                phase:
                  type: string
                  enum: [Pending, Bound, Error]
                message:
                  type: string
                resolvedEndpoint:
                  type: string
                lastResolvedTime:
                  type: string
                  format: date-time
                conditions:
                  type: array
                  items:
                    type: object
                    required: [type, status]
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                        enum: ["True", "False", "Unknown"]
                      reason:
                        type: string
                      message:
                        type: string
                      lastTransitionTime:
                        type: string
                        format: date-time
```
