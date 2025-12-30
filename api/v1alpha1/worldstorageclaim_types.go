package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type WorldStorageScope string

const (
	WorldStorageScopeWorld      WorldStorageScope = "world"
	WorldStorageScopeWorldShard WorldStorageScope = "world-shard"
)

type WorldStorageTier string

const (
	WorldStorageTierServerLowLatency  WorldStorageTier = "server-low-latency"
	WorldStorageTierServerHighLatency WorldStorageTier = "server-high-latency"
	WorldStorageTierClientLowLatency  WorldStorageTier = "client-low-latency"
)

// WorldStorageClaim declares a world (or world-shard) scoped persistent storage requirement.
//
// The StorageOrchestrator controller materializes a backing PVC for server-side tiers.
// For client-side tiers, it publishes an External status (the storage is outside the cluster).
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=wsc
// +kubebuilder:printcolumn:name="Scope",type=string,JSONPath=`.spec.scope`
// +kubebuilder:printcolumn:name="Tier",type=string,JSONPath=`.spec.tier`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="PVC",type=string,JSONPath=`.status.claimName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type WorldStorageClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorldStorageClaimSpec   `json:"spec"`
	Status WorldStorageClaimStatus `json:"status,omitempty"`
}

type WorldStorageClaimSpec struct {
	Scope WorldStorageScope `json:"scope"`
	Tier  WorldStorageTier  `json:"tier"`

	WorldRef ObjectRef  `json:"worldRef"`
	ShardRef *ObjectRef `json:"shardRef,omitempty"`

	// Size is a Kubernetes quantity string like "10Gi".
	Size string `json:"size"`

	// AccessModes are Kubernetes access modes (e.g., ReadWriteOnce).
	AccessModes []string `json:"accessModes,omitempty"`

	// Optional explicit storage class name. If empty, StorageOrchestrator may choose a default.
	StorageClassName string `json:"storageClassName,omitempty"`
}

type WorldStorageClaimStatus struct {
	Phase            string `json:"phase,omitempty"`
	Message          string `json:"message,omitempty"`
	ClaimName        string `json:"claimName,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
	ExternalURI      string `json:"externalUri,omitempty"`
}

// +kubebuilder:object:root=true
type WorldStorageClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorldStorageClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorldStorageClaim{}, &WorldStorageClaimList{})
}
