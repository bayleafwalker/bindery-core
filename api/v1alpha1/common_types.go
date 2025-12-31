package v1alpha1

// NOTE: These types are intentionally minimal for the skeleton.
// They model only the fields needed for the controller wiring.

type CapabilityScope string

type CapabilityMultiplicity string

type DependencyMode string

const (
	CapabilityScopeCluster    CapabilityScope = "cluster"
	CapabilityScopeRegion     CapabilityScope = "region"
	CapabilityScopeRealm      CapabilityScope = "realm"
	CapabilityScopeWorld      CapabilityScope = "world"
	CapabilityScopeWorldShard CapabilityScope = "world-shard"
	CapabilityScopeSession    CapabilityScope = "session"

	MultiplicityOne  CapabilityMultiplicity = "1"
	MultiplicityMany CapabilityMultiplicity = "many"

	DependencyModeRequired DependencyMode = "required"
	DependencyModeOptional DependencyMode = "optional"
)

type WorldRef struct {
	Name string `json:"name,omitempty"`
}

type ObjectRef struct {
	Name string `json:"name"`
}

type EndpointRef struct {
	Type  string `json:"type,omitempty"`
	Value string `json:"value,omitempty"`
	Port  int32  `json:"port,omitempty"`
}
