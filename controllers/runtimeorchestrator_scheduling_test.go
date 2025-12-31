package controllers

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	binderyv1alpha1 "github.com/bayleafwalker/bindery-core/api/v1alpha1"
)

func TestRuntimeOrchestrator_PropagatesSchedulingConstraints(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = binderyv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	world := &binderyv1alpha1.WorldInstance{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "sched-world", Namespace: "bindery-sched"},
		Spec:       binderyv1alpha1.WorldInstanceSpec{BookletRef: binderyv1alpha1.ObjectRef{Name: "sched-game"}, WorldID: "world-sched", Region: "us-sched", ShardCount: 1},
	}

	priorityClass := "high-priority-game"
	provider := &binderyv1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sched-module",
			Namespace: "bindery-sched",
			Annotations: map[string]string{
				annRuntimeImage: "alpine:3.20",
				annRuntimePort:  "8080",
			},
		},
		Spec: binderyv1alpha1.ModuleManifestSpec{
			Module: binderyv1alpha1.ModuleIdentity{ID: "sched.mod", Version: "1.0.0"},
			Scheduling: binderyv1alpha1.ModuleScheduling{
				PriorityClassName: priorityClass,
				NodeSelector:      map[string]string{"node-type": "game-server"},
				Tolerations: []corev1.Toleration{
					{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "game", Effect: corev1.TaintEffectNoSchedule},
				},
			},
		},
	}

	binding := &binderyv1alpha1.CapabilityBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "CapabilityBinding"},
		ObjectMeta: metav1.ObjectMeta{Name: "sched-binding", Namespace: "bindery-sched"},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "sched.cap",
			Scope:        binderyv1alpha1.CapabilityScopeWorldShard,
			Multiplicity: binderyv1alpha1.MultiplicityOne,
			WorldRef:     &binderyv1alpha1.WorldRef{Name: "sched-world"},
			Consumer:     binderyv1alpha1.ConsumerRef{ModuleManifestName: "consumer"},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: "sched-module"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world, provider, binding).WithStatusSubresource(binding).Build()

	r := &RuntimeOrchestratorReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "bindery-sched", Name: "sched-binding"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	workloadName := rtName(world.Name, provider.Name)
	var dep appsv1.Deployment
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-sched", Name: workloadName}, &dep); err != nil {
		t.Fatalf("expected deployment: %v", err)
	}

	if dep.Spec.Template.Spec.PriorityClassName != priorityClass {
		t.Errorf("Expected PriorityClassName %q, got %q", priorityClass, dep.Spec.Template.Spec.PriorityClassName)
	}

	if dep.Spec.Template.Spec.NodeSelector["node-type"] != "game-server" {
		t.Errorf("Expected NodeSelector node-type=game-server, got %v", dep.Spec.Template.Spec.NodeSelector)
	}

	foundToleration := false
	for _, tol := range dep.Spec.Template.Spec.Tolerations {
		if tol.Key == "dedicated" && tol.Value == "game" && tol.Effect == corev1.TaintEffectNoSchedule {
			foundToleration = true
			break
		}
	}
	if !foundToleration {
		t.Errorf("Expected toleration not found in %v", dep.Spec.Template.Spec.Tolerations)
	}
}
