package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Booklet declares a game composed of modules.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=bk
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="BookletID",type=string,JSONPath=`.spec.bookletId`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
//
// NOTE: This skeleton defines only a subset of fields.
type Booklet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BookletSpec   `json:"spec"`
	Status BookletStatus `json:"status,omitempty"`
}

type BookletSpec struct {
	BookletID  string             `json:"bookletId"`
	Version    string             `json:"version"`
	Modules    []BookletModuleRef `json:"modules"`
	Colocation []ColocationGroup  `json:"colocation,omitempty"`
	Defaults   map[string]string  `json:"-"` // TODO: expand
}

type ColocationGroup struct {
	Name     string   `json:"name"`
	Modules  []string `json:"modules"`
	Strategy string   `json:"strategy"` // "Node" or "Pod"
}

type BookletModuleRef struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
}

type BookletStatus struct {
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
type BookletList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Booklet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Booklet{}, &BookletList{})
}
