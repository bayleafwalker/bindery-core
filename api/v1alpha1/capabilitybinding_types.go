package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CapabilityBinding declares an explicit binding from a consumer requirement to a provider capability.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=capbind
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Capability",type=string,JSONPath=`.spec.capabilityId`
// +kubebuilder:printcolumn:name="Scope",type=string,JSONPath=`.spec.scope`
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider.moduleManifestName`
// +kubebuilder:printcolumn:name="Consumer",type=string,JSONPath=`.spec.consumer.moduleManifestName`
// +kubebuilder:printcolumn:name="World",type=string,JSONPath=`.spec.worldRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
//
// NOTE: This skeleton defines only a subset of fields.
type CapabilityBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CapabilityBindingSpec   `json:"spec"`
	Status CapabilityBindingStatus `json:"status,omitempty"`
}

type CapabilityBindingSpec struct {
	CapabilityID string                 `json:"capabilityId"`
	Scope        CapabilityScope        `json:"scope"`
	Multiplicity CapabilityMultiplicity `json:"multiplicity"`
	WorldRef     *WorldRef              `json:"worldRef,omitempty"`
	Consumer     ConsumerRef            `json:"consumer"`
	Provider     ProviderRef            `json:"provider"`
}

type ConsumerRef struct {
	ModuleManifestName string           `json:"moduleManifestName"`
	Requirement        *RequirementHint `json:"requirement,omitempty"`
}

type RequirementHint struct {
	VersionConstraint string         `json:"versionConstraint,omitempty"`
	DependencyMode    DependencyMode `json:"dependencyMode,omitempty"`
	RequiredFeatures  []string       `json:"requiredFeatures,omitempty"`
	PreferredFeatures []string       `json:"preferredFeatures,omitempty"`
}

type ProviderRef struct {
	ModuleManifestName string `json:"moduleManifestName"`
	CapabilityVersion  string `json:"capabilityVersion,omitempty"`
}

type CapabilityBindingStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Phase              string             `json:"phase,omitempty"`
	Message            string             `json:"message,omitempty"`
	Provider           *ProviderStatus    `json:"provider,omitempty"`
	ResolvedEndpoint   string             `json:"resolvedEndpoint,omitempty"`
	LastResolvedTime   *metav1.Time       `json:"lastResolvedTime,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

type ProviderStatus struct {
	Endpoint *EndpointRef `json:"endpoint,omitempty"`
}

// +kubebuilder:object:root=true
type CapabilityBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CapabilityBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CapabilityBinding{}, &CapabilityBindingList{})
}
