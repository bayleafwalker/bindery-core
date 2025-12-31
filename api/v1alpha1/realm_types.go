package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Realm defines a shared context for multiple WorldInstances.
// It manages global services and shared configuration.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Realm struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RealmSpec   `json:"spec,omitempty"`
	Status RealmStatus `json:"status,omitempty"`
}

type RealmSpec struct {
	// Modules lists the global modules that should be running in this Realm.
	// These are resolved and deployed by the RealmController.
	Modules []RealmModule `json:"modules,omitempty"`
}

type RealmModule struct {
	// Name of the ModuleManifest to deploy.
	Name string `json:"name"`
	// Version of the module (optional, defaults to latest/any if not specified, but usually required for determinism).
	Version string `json:"version,omitempty"`
}

type RealmStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// RealmList contains a list of Realm
// +kubebuilder:object:root=true
type RealmList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Realm `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Realm{}, &RealmList{})
}
