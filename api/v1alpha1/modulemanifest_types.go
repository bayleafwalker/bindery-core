package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
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
	Module     ModuleIdentity       `json:"module"`
	Runtime    *ModuleRuntimeSpec   `json:"runtime,omitempty"`
	Provides   []ProvidedCapability `json:"provides"`
	Requires   []RequiredCapability `json:"requires"`
	Scaling    ModuleScaling        `json:"scaling"`
	Scheduling ModuleScheduling     `json:"scheduling,omitempty"`
	ExtraSpec  map[string]any       `json:"-"` // TODO: expand spec per CRD
}

type ModuleScheduling struct {
	Affinity          *corev1.Affinity    `json:"affinity,omitempty"`
	Tolerations       []corev1.Toleration `json:"tolerations,omitempty"`
	NodeSelector      map[string]string   `json:"nodeSelector,omitempty"`
	PriorityClassName string              `json:"priorityClassName,omitempty"`
}

type ModuleRuntimeSpec struct {
	// Image is the container image to deploy for this module.
	//
	// If empty, the module is treated as not server-orchestrated (or may rely on legacy annotations).
	Image string `json:"image,omitempty"`

	// Port is the gRPC port exposed by the module container.
	Port *int32 `json:"port,omitempty"`

	// Command and Args correspond to the Kubernetes container command/args fields.
	Command []string `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`

	// Env is a simple string map merged with platform-injected discovery variables.
	Env map[string]string `json:"env,omitempty"`

	// TerminationGracePeriodSeconds controls how long Kubernetes waits before SIGKILL.
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`

	// PreStopCommand runs as a PreStop hook via `/bin/sh -c <command>`.
	PreStopCommand string `json:"preStopCommand,omitempty"`
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
