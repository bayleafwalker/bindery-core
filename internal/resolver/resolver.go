package resolver

import "context"

// Resolver computes a Plan (desired bindings) for a given Input.
//
// Business logic (semver matching, scope/multiplicity rules, deterministic selection)
// will be implemented in a later iteration.
type Resolver interface {
	Resolve(ctx context.Context, in Input) (Plan, error)
}
