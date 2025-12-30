package controllers

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
)

const (
	WorldConditionModulesResolved  = "ModulesResolved"
	WorldConditionBindingsResolved = "BindingsResolved"
	WorldConditionRuntimeReady     = "RuntimeReady"

	BindingConditionRuntimeReady = "RuntimeReady"
)

func setWorldCondition(world *gamev1alpha1.WorldInstance, condition metav1.Condition) {
	if world == nil {
		return
	}
	condition.ObservedGeneration = world.Generation
	meta.SetStatusCondition(&world.Status.Conditions, condition)
}

func setBindingCondition(binding *gamev1alpha1.CapabilityBinding, condition metav1.Condition) {
	if binding == nil {
		return
	}
	condition.ObservedGeneration = binding.Generation
	meta.SetStatusCondition(&binding.Status.Conditions, condition)
}

func runtimeReadyMessage(readyCount, totalCount int) string {
	if totalCount <= 0 {
		return "No server workloads required"
	}
	return fmt.Sprintf("%d/%d server workloads have published endpoints", readyCount, totalCount)
}
