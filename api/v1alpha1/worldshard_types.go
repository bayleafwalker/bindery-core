package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorldShard represents an explicit shard (partition) of a WorldInstance.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=shard
// +kubebuilder:printcolumn:name="World",type=string,JSONPath=`.spec.worldRef.name`
// +kubebuilder:printcolumn:name="Shard",type=integer,JSONPath=`.spec.shardId`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type WorldShard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorldShardSpec   `json:"spec"`
	Status WorldShardStatus `json:"status,omitempty"`
}

type WorldShardSpec struct {
	WorldRef ObjectRef `json:"worldRef"`
	ShardID  int32     `json:"shardId"`
}

type WorldShardStatus struct {
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
type WorldShardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorldShard `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorldShard{}, &WorldShardList{})
}
