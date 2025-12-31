package resolver

import (
	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
)

// Input is the controller-normalized view of the world that the resolver operates on.
//
// This is intentionally minimal and can be extended as the platform evolves.
type Input struct {
	World   gamev1alpha1.WorldInstance
	Game    gamev1alpha1.GameDefinition
	Modules []gamev1alpha1.ModuleManifest
	// ExternalModules are modules available in the wider context (e.g. Realm/Cluster)
	// that can satisfy requirements but are not part of the GameDefinition itself.
	ExternalModules []gamev1alpha1.ModuleManifest
}

// Plan is the desired output of the resolver.
//
// In the skeleton, we only model desired CapabilityBindings.
// (Later we can add status projections, diagnostics, etc.)
type Plan struct {
	DesiredBindings []gamev1alpha1.CapabilityBinding
	Diagnostics     Diagnostics
}

// Diagnostics captures human-readable information about resolution.
//
// This is useful for status/messages/events, and for logging.
type Diagnostics struct {
	UnresolvedRequired []UnresolvedRequirement
	UnresolvedOptional []UnresolvedRequirement
}

type UnresolvedRequirement struct {
	ConsumerModuleManifestName string
	CapabilityID               string
	Scope                      gamev1alpha1.CapabilityScope
	Reason                     string
}
