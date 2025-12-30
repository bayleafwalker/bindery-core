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

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
)

func TestRuntimeOrchestrator_CreatesServiceDeploymentAndPublishesEndpoint(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(client-go): %v", err)
	}
	if err := gamev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(game): %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(apps): %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core): %v", err)
	}

	world := &gamev1alpha1.WorldInstance{
		TypeMeta:   metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "anvil-sample-world", Namespace: "anvil-demo", UID: types.UID("world-uid")},
		Spec:       gamev1alpha1.WorldInstanceSpec{GameRef: gamev1alpha1.ObjectRef{Name: "anvil-sample"}, WorldID: "world-001", Region: "us-test-1", ShardCount: 1},
	}

	provider := &gamev1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "core-physics-engine",
			Namespace: "anvil-demo",
			Annotations: map[string]string{
				annRuntimeImage: "alpine:3.20",
				annRuntimePort:  "50051",
			},
		},
		Spec: gamev1alpha1.ModuleManifestSpec{Module: gamev1alpha1.ModuleIdentity{ID: "core.physics", Version: "1.3.0"}},
	}

	binding := &gamev1alpha1.CapabilityBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "CapabilityBinding"},
		ObjectMeta: metav1.ObjectMeta{Name: "binding-1", Namespace: "anvil-demo"},
		Spec: gamev1alpha1.CapabilityBindingSpec{
			CapabilityID: "physics.engine",
			Scope:        gamev1alpha1.CapabilityScopeWorldShard,
			Multiplicity: gamev1alpha1.MultiplicityOne,
			WorldRef:     &gamev1alpha1.WorldRef{Name: "anvil-sample-world"},
			Consumer:     gamev1alpha1.ConsumerRef{ModuleManifestName: "core-interaction-engine"},
			Provider:     gamev1alpha1.ProviderRef{ModuleManifestName: "core-physics-engine", CapabilityVersion: "1.2.0"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world, provider, binding).WithStatusSubresource(binding).Build()

	r := &RuntimeOrchestratorReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "anvil-demo", Name: "binding-1"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	workloadName := rtName(world.Name, provider.Name)

	var svc corev1.Service
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "anvil-demo", Name: workloadName}, &svc); err != nil {
		t.Fatalf("expected service: %v", err)
	}

	var dep appsv1.Deployment
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "anvil-demo", Name: workloadName}, &dep); err != nil {
		t.Fatalf("expected deployment: %v", err)
	}

	var got gamev1alpha1.CapabilityBinding
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "anvil-demo", Name: "binding-1"}, &got); err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if got.Status.Provider == nil || got.Status.Provider.Endpoint == nil {
		t.Fatalf("expected endpoint to be set")
	}
	if got.Status.Provider.Endpoint.Type != "kubernetesService" || got.Status.Provider.Endpoint.Value != workloadName {
		t.Fatalf("unexpected endpoint: %#v", got.Status.Provider.Endpoint)
	}
}
