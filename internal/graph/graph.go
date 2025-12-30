package graph

// Package graph contains scaffolding for building dependency graphs between modules and capabilities.
//
// The controller will use these types to model the resolution problem, but no logic is implemented yet.

type CapabilityKey struct {
	CapabilityID string
	Scope        string
}

type ProviderNode struct {
	ModuleManifestName string
	CapabilityKey      CapabilityKey
	CapabilityVersion  string
	Multiplicity       string
}

type RequirementNode struct {
	ModuleManifestName string
	CapabilityKey      CapabilityKey
	VersionConstraint  string
	DependencyMode     string
	Multiplicity       string
}

type DependencyGraph struct {
	Providers    []ProviderNode
	Requirements []RequirementNode
}
