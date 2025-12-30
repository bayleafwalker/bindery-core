# CapabilityResolver Controller Design (v1alpha1)

This document defines a Kubernetes controller named **CapabilityResolver**.

**Purpose:** For each `WorldInstance`, compute a satisfiable set of `CapabilityBinding` resources that bind module requirements (`requires[]`) to module capabilities (`provides[]`) according to the platform standards.

## Inputs / Outputs

**Watches (primary inputs):**
- `ModuleManifest` (namespaced)
- `GameDefinition` (namespaced)
- `WorldInstance` (namespaced)

**Produces (primary outputs):**
- `CapabilityBinding` (namespaced)

**May update status for debuggability:**
- `WorldInstance.status` (recommended)
- `CapabilityBinding.status` (recommended)

## Core concepts

### Resolution unit

The controller reconciles **per WorldInstance**.

- `WorldInstance.spec.gameRef.name` selects a `GameDefinition`.
- `GameDefinition.spec.modules[]` selects the set of `ModuleManifest` resources that participate in the world.
- Bindings are computed for that world and should include `spec.worldRef.name`.

### Compatibility rules

The resolver enforces:

1) **SemVer compatibility**
- For each consumer requirement (`ModuleManifest.spec.requires[].versionConstraint`), the chosen provider’s `ModuleManifest.spec.provides[].version` must satisfy the constraint.
- If multiple providers satisfy the constraint, the resolver chooses deterministically (see “Provider selection”).

2) **Scope compatibility**
- `ModuleManifest.spec.requires[].scope` must match `ModuleManifest.spec.provides[].scope`.
- `CapabilityBinding.spec.scope` must equal that scope.

3) **Multiplicity compatibility**
- If the requirement multiplicity is `"1"`, the resolver produces **exactly one** `CapabilityBinding` for that requirement.
- If the requirement multiplicity is `many`, the resolver may produce:
  - a single `CapabilityBinding` to a provider that advertises `many` (representing a pool), OR
  - multiple `CapabilityBinding`s if/when provider instances become explicit.

Compatibility matrix (v1alpha1):
- require `"1"` → provider `"1"` or `many` (resolver selects one provider)
- require `many` → provider must be `many`

4) **Graceful failure**
- Missing/unsatisfied **required** requirements must be surfaced clearly (status + events), without crashing or “half-writing” invalid objects.
- Missing **optional** requirements should not block other bindings.

## High-level algorithm

For each `WorldInstance` reconcile:

1) Load `WorldInstance`.
2) Load referenced `GameDefinition`.
3) Resolve the participating `ModuleManifest` set from `GameDefinition.spec.modules[]`.
4) Build an index of providers by `(capabilityId, scope)`.
5) For each module in the game:
   - For each `requires[]` entry:
     - Find candidate providers from the index.
     - Filter by SemVer constraint, scope, and multiplicity compatibility.
     - Choose a provider deterministically.
     - Create/update the corresponding `CapabilityBinding`.
6) Garbage-collect stale `CapabilityBinding`s that are owned by this world but are no longer desired.
7) Update `WorldInstance.status` to reflect whether all required requirements are satisfied.

### Provider selection (deterministic)

If multiple providers match a requirement:

1) Prefer provider with the **highest** capability version satisfying the constraint.
2) If still tied, prefer provider whose `ModuleManifest.metadata.name` is lexicographically smallest.

This ensures stable outputs across reconciles.

## Pseudocode

The pseudocode below is intentionally implementation-agnostic (it maps cleanly to controller-runtime in Go).

```text
reconcile(worldKey):
  world = get(WorldInstance, worldKey)
  if world not found:
    return

  game = get(GameDefinition, (world.namespace, world.spec.gameRef.name))
  if game not found:
    setWorldCondition(world, type="BindingsResolved", status=False,
                      reason="GameDefinitionNotFound")
    return

  moduleNames = [m.name for m in game.spec.modules]

  modules = []
  for name in moduleNames:
    mm = get(ModuleManifest, (world.namespace, name))
    if mm not found:
      recordMissingModule(name)
    else:
      modules.append(mm)

  # Index providers
  providers = map[(capabilityId, scope)] -> list of ProviderEntry
  for mm in modules:
    for prov in mm.spec.provides:
      providers[(prov.capabilityId, prov.scope)].append({
        moduleManifestName: mm.metadata.name,
        capabilityVersion: prov.version,
        multiplicity: prov.multiplicity,
      })

  desiredBindings = set()
  unresolvedRequired = []

  for consumerMM in modules:
    for req in consumerMM.spec.requires:
      key = (req.capabilityId, req.scope)
      candidates = providers.get(key, [])

      candidates = filter(candidates, lambda p: semverSatisfies(req.versionConstraint, p.capabilityVersion))
      candidates = filter(candidates, lambda p: multiplicityCompatible(req.multiplicity, p.multiplicity))

      if candidates is empty:
        if req.dependencyMode == "required":
          unresolvedRequired.append({consumer: consumerMM.metadata.name, requirement: req})
        else:
          # optional: no binding, but record for status visibility
          recordUnresolvedOptional(consumerMM, req)
        continue

      chosen = chooseDeterministically(candidates)

      bindingName = stableBindingName(world.metadata.name, consumerMM.metadata.name, req.capabilityId)

      binding = desired CapabilityBinding:
        metadata:
          name: bindingName
          namespace: world.namespace
          ownerReferences: [world]
          labels:
            game.platform/world: world.metadata.name
            game.platform/game: game.metadata.name
            game.platform/capabilityId: req.capabilityId
        spec:
          capabilityId: req.capabilityId
          scope: req.scope
          multiplicity: req.multiplicity
          worldRef:
            name: world.metadata.name
          consumer:
            moduleManifestName: consumerMM.metadata.name
            requirement:
              versionConstraint: req.versionConstraint
              dependencyMode: req.dependencyMode
          provider:
            moduleManifestName: chosen.moduleManifestName
            capabilityVersion: chosen.capabilityVersion

      apply(binding)  # server-side apply recommended
      desiredBindings.add(bindingName)

  deleteStaleBindings(world, keepNames=desiredBindings)

  if unresolvedRequired non-empty:
    setWorldCondition(world, type="BindingsResolved", status=False, reason="UnresolvedRequired")
    setWorldPhase(world, "Error")
    emitEvent(world, type=Warning, reason="UnresolvedBindings", message=summarize(unresolvedRequired))
  else:
    setWorldCondition(world, type="BindingsResolved", status=True, reason="AllResolved")
    setWorldPhase(world, "Running")
    emitEvent(world, type=Normal, reason="BindingsResolved", message="All required bindings resolved")

  updateStatus(world)
```

## Error handling behavior

### Failure categories

1) **Transient API errors** (timeouts, conflicts, cache not ready)
- Behavior: return error to requeue; do not change status unless safe.

2) **Missing inputs**
- Missing `GameDefinition`: set `WorldInstance.status.phase=Error` and condition `BindingsResolved=False` with reason `GameDefinitionNotFound`.
- Missing `ModuleManifest` referenced by the game: set `BindingsResolved=False` with reason `ModuleManifestNotFound` and list missing names in `WorldInstance.status.message`.

3) **Unsatisfied requirements**
- Required requirement has no candidate provider → `BindingsResolved=False`, reason `UnresolvedRequired`.
- Optional requirement unsatisfied → keep `BindingsResolved=True` if all required are satisfied, but include a warning in `WorldInstance.status.message`.

4) **Invalid specs** (schema-valid but semantically invalid)
Examples:
- `versionConstraint` not parseable as a SemVer range
- unknown `scope` string (should not happen with CRD validation)

Behavior:
- Treat as a configuration error.
- Mark `WorldInstance` as `Error` and surface the parsing error in status.

### Idempotency

The controller must be fully idempotent:
- Re-running reconcile produces the same set of bindings (given stable inputs).
- Partial progress is okay (some bindings created, others pending) as long as status reflects unresolved required requirements.

## Eventual consistency model

- The system is **eventually consistent**.
- Any change to inputs (`ModuleManifest`, `GameDefinition`, `WorldInstance`) will eventually result in the desired set of `CapabilityBinding` resources.
- Convergence properties:
  - deterministic provider selection avoids “binding churn”
  - reconcile is level-based (“desired state”), not edge-based
  - stale bindings are garbage-collected

Recommended mechanics:
- Use shared informers (cached reads).
- Reconcile triggers:
  - direct watch on `WorldInstance`
  - watch `GameDefinition` and enqueue all worlds referencing it
  - watch `ModuleManifest` and enqueue all worlds whose `GameDefinition` references it

## Debuggability and observability

### Status surfaces

**WorldInstance.status** (recommended additions within existing schema):
- `status.phase`: `Running` only when all required bindings resolved
- `status.message`: concise summary of unresolved requirements
- `status.conditions[]`:
  - `type: BindingsResolved` (`True/False`)
  - `type: ModulesResolved` (`True/False`)

**CapabilityBinding.status** (recommended):
- `phase`: `Pending` until provider endpoint is known, then `Bound`
- `message`: last resolution decision, errors
- `resolvedEndpoint`: a concrete endpoint string if/when available

### Kubernetes Events

Emit events on `WorldInstance`:
- Normal `BindingsResolved` when all required bindings resolve
- Warning `UnresolvedBindings` when required bindings cannot be satisfied
- Warning `InvalidSemverConstraint` when constraint parsing fails

### Logging

Structured log fields (at minimum):
- `world`, `namespace`, `game`
- `consumerModule`, `capabilityId`, `scope`, `multiplicity`
- `candidateCount`, `chosenProvider`, `chosenVersion`

### Metrics

Suggested Prometheus metrics:
- `capabilityresolver_reconcile_total{result=success|error}`
- `capabilityresolver_unresolved_required_total`
- `capabilityresolver_bindings_desired`
- `capabilityresolver_bindings_applied_total`

### Tracing

If using OpenTelemetry:
- span per reconcile with attributes: `world`, `game`, `desiredBindings`
- child spans for provider selection per requirement

## `CapabilityBinding` YAML examples

These are minimal examples that fit the v1alpha1 `CapabilityBinding` CRD.

### Example A: physics → timeSource

Assumptions:
- The physics module requires `time.source` in `world` scope.
- A `core-time-source` module provides `time.source`.

```yaml
apiVersion: game.platform/v1alpha1
kind: CapabilityBinding
metadata:
  name: physics-requires-timesource
  namespace: anvil-demo
spec:
  capabilityId: time.source
  scope: world
  multiplicity: "1"
  worldRef:
    name: anvil-sample-world
  consumer:
    moduleManifestName: core-physics-engine
    requirement:
      versionConstraint: "^1.0.0"
      dependencyMode: required
  provider:
    moduleManifestName: core-time-source
    capabilityVersion: "1.0.0"
```

### Example B: interaction → physics

```yaml
apiVersion: game.platform/v1alpha1
kind: CapabilityBinding
metadata:
  name: interaction-requires-physics
  namespace: anvil-demo
spec:
  capabilityId: physics.engine
  scope: world
  multiplicity: "1"
  worldRef:
    name: anvil-sample-world
  consumer:
    moduleManifestName: core-interaction-engine
    requirement:
      versionConstraint: "^1.0.0"
      dependencyMode: required
  provider:
    moduleManifestName: core-physics-engine
    capabilityVersion: "1.0.0"
```
