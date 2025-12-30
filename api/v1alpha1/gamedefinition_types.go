package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GameDefinition declares a game composed of modules.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=game
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="GameID",type=string,JSONPath=`.spec.gameId`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
//
// NOTE: This skeleton defines only a subset of fields.
type GameDefinition struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GameDefinitionSpec   `json:"spec"`
	Status GameDefinitionStatus `json:"status,omitempty"`
}

type GameDefinitionSpec struct {
	GameID   string            `json:"gameId"`
	Version  string            `json:"version"`
	Modules  []GameModuleRef   `json:"modules"`
	Defaults map[string]string `json:"-"` // TODO: expand
}

type GameModuleRef struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
}

type GameDefinitionStatus struct {
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
type GameDefinitionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GameDefinition `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GameDefinition{}, &GameDefinitionList{})
}
