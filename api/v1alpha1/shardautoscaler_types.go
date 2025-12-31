package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ShardAutoscaler automatically scales the number of shards for a WorldInstance.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=sa
// +kubebuilder:printcolumn:name="World",type=string,JSONPath=`.spec.worldRef.name`
// +kubebuilder:printcolumn:name="Min",type=integer,JSONPath=`.spec.minShards`
// +kubebuilder:printcolumn:name="Max",type=integer,JSONPath=`.spec.maxShards`
// +kubebuilder:printcolumn:name="Current",type=integer,JSONPath=`.status.currentShards`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ShardAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ShardAutoscalerSpec   `json:"spec"`
	Status ShardAutoscalerStatus `json:"status,omitempty"`
}

type ShardAutoscalerSpec struct {
	// WorldRef references the WorldInstance to scale.
	WorldRef ObjectRef `json:"worldRef"`

	// MinShards is the minimum number of shards.
	// +kubebuilder:validation:Minimum=1
	MinShards int32 `json:"minShards"`

	// MaxShards is the maximum number of shards.
	// +kubebuilder:validation:Minimum=1
	MaxShards int32 `json:"maxShards"`

	// Metrics defines the metrics to use for scaling.
	// Currently only supports CPU and Memory utilization.
	Metrics []MetricSpec `json:"metrics,omitempty"`
}

type MetricSpec struct {
	// Type is the type of metric (e.g., "Resource").
	Type string `json:"type"`

	// Resource defines the resource metric.
	Resource *ResourceMetricSource `json:"resource,omitempty"`
}

type ResourceMetricSource struct {
	// Name is the name of the resource (cpu, memory).
	Name string `json:"name"`

	// TargetAverageUtilization is the target value of the average of the
	// resource metric across all relevant pods, represented as a percentage of
	// the requested value of the resource for the pods.
	TargetAverageUtilization *int32 `json:"targetAverageUtilization,omitempty"`
}

type ShardAutoscalerStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	CurrentShards      int32              `json:"currentShards"`
	DesiredShards      int32              `json:"desiredShards"`
	LastScaleTime      *metav1.Time       `json:"lastScaleTime,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type ShardAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ShardAutoscaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ShardAutoscaler{}, &ShardAutoscalerList{})
}
