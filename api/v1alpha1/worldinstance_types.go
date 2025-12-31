package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorldInstance instantiates a world from a Booklet.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=world
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="WorldID",type=string,JSONPath=`.spec.worldId`
// +kubebuilder:printcolumn:name="Game",type=string,JSONPath=`.spec.gameRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
//
// NOTE: This skeleton defines only a subset of fields.
type WorldInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorldInstanceSpec   `json:"spec"`
	Status WorldInstanceStatus `json:"status,omitempty"`
}

type WorldInstanceSpec struct {
	GameRef ObjectRef `json:"gameRef"`
	// RealmRef identifies the Realm this world belongs to.
	// If not specified, the world is considered standalone or part of a default realm.
	RealmRef     *ObjectRef `json:"realmRef,omitempty"`
	WorldID      string     `json:"worldId"`
	Region       string     `json:"region"`
	ShardCount   int32      `json:"shardCount"`
	DesiredState string     `json:"desiredState,omitempty"`
}

type WorldInstanceStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Phase              string             `json:"phase,omitempty"`
	Message            string             `json:"message,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type WorldInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorldInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorldInstance{}, &WorldInstanceList{})
}
