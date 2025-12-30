package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ModuleManifest declares a module's identity and its provides/requires contracts.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=mm
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Module",type=string,JSONPath=`.spec.module.id`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.module.version`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
//
// NOTE: This skeleton defines only a subset of fields.
// The full spec lives in the docs/CRDs and can be expanded later.
type ModuleManifest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleManifestSpec   `json:"spec"`
	Status ModuleManifestStatus `json:"status,omitempty"`
}

type ModuleManifestSpec struct {
	Module    ModuleIdentity       `json:"module"`
	Provides  []ProvidedCapability `json:"provides"`
	Requires  []RequiredCapability `json:"requires"`
	Scaling   ModuleScaling        `json:"scaling"`
	ExtraSpec map[string]any       `json:"-"` // TODO: expand spec per CRD
}

type ModuleIdentity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type ProvidedCapability struct {
	CapabilityID string                 `json:"capabilityId"`
	Version      string                 `json:"version"`
	Scope        CapabilityScope        `json:"scope"`
	Multiplicity CapabilityMultiplicity `json:"multiplicity"`
}

type RequiredCapability struct {
	CapabilityID      string                 `json:"capabilityId"`
	VersionConstraint string                 `json:"versionConstraint"`
	Scope             CapabilityScope        `json:"scope"`
	Multiplicity      CapabilityMultiplicity `json:"multiplicity"`
	DependencyMode    DependencyMode         `json:"dependencyMode"`
}

type ModuleScaling struct {
	DefaultScope CapabilityScope `json:"defaultScope"`
	Statefulness string          `json:"statefulness"`
}

type ModuleManifestStatus struct {
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
type ModuleManifestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModuleManifest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModuleManifest{}, &ModuleManifestList{})
}
