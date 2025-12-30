package controllers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
)

func TestIntegration_RuntimeOrchestrator_PublishesEndpointToStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short")
	}
	if os.Getenv("ANVIL_INTEGRATION") != "1" {
		t.Skip("set ANVIL_INTEGRATION=1 (or run `make test-integration`) to enable envtest integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	crdDir := filepath.Join("..", "k8s", "crds")
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{crdDir},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		// envtest requires apiserver/etcd binaries. We keep the error actionable.
		t.Fatalf("start envtest (set KUBEBUILDER_ASSETS; try `make test-integration`): %v", err)
	}
	defer func() {
		_ = testEnv.Stop()
	}()

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

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "anvil-integration"}}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	world := &gamev1alpha1.WorldInstance{
		TypeMeta:   metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: ns.Name},
		Spec: gamev1alpha1.WorldInstanceSpec{
			GameRef:      gamev1alpha1.ObjectRef{Name: "game-1"},
			WorldID:      "world-001",
			Region:       "test-1",
			ShardCount:   1,
			DesiredState: "Running",
		},
	}
	if err := k8sClient.Create(ctx, world); err != nil {
		t.Fatalf("create world: %v", err)
	}

	provider := &gamev1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "core-physics-engine",
			Namespace: ns.Name,
			Annotations: map[string]string{
				annRuntimeImage: "alpine:3.20",
				annRuntimePort:  "50051",
			},
		},
		Spec: gamev1alpha1.ModuleManifestSpec{
			Module: gamev1alpha1.ModuleIdentity{ID: "core.physics", Version: "1.3.0"},
			Provides: []gamev1alpha1.ProvidedCapability{{
				CapabilityID: "physics.engine",
				Version:      "1.3.0",
				Scope:        gamev1alpha1.CapabilityScopeWorldShard,
				Multiplicity: gamev1alpha1.MultiplicityOne,
			}},
			Requires: []gamev1alpha1.RequiredCapability{},
			Scaling:  gamev1alpha1.ModuleScaling{DefaultScope: gamev1alpha1.CapabilityScopeWorldShard, Statefulness: "stateless"},
		},
	}
	if err := k8sClient.Create(ctx, provider); err != nil {
		t.Fatalf("create provider modulemanifest: %v", err)
	}

	binding := &gamev1alpha1.CapabilityBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "CapabilityBinding"},
		ObjectMeta: metav1.ObjectMeta{Name: "binding-1", Namespace: ns.Name},
		Spec: gamev1alpha1.CapabilityBindingSpec{
			CapabilityID: "physics.engine",
			Scope:        gamev1alpha1.CapabilityScopeWorldShard,
			Multiplicity: gamev1alpha1.MultiplicityOne,
			WorldRef:     &gamev1alpha1.WorldRef{Name: world.Name},
			Consumer:     gamev1alpha1.ConsumerRef{ModuleManifestName: "core-interaction-engine"},
			Provider:     gamev1alpha1.ProviderRef{ModuleManifestName: provider.Name, CapabilityVersion: "1.3.0"},
		},
	}
	if err := k8sClient.Create(ctx, binding); err != nil {
		t.Fatalf("create binding: %v", err)
	}

	r := &RuntimeOrchestratorReconciler{Client: k8sClient, Scheme: scheme}
	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns.Name, Name: binding.Name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	workloadName := rtName(world.Name, provider.Name)

	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for runtime resources + status")
		}

		var svc corev1.Service
		svcErr := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, &svc)

		var dep appsv1.Deployment
		depErr := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, &dep)

		var got gamev1alpha1.CapabilityBinding
		bindErr := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: binding.Name}, &got)

		ready := svcErr == nil && depErr == nil && bindErr == nil &&
			got.Status.Provider != nil && got.Status.Provider.Endpoint != nil &&
			got.Status.Provider.Endpoint.Type == "kubernetesService" &&
			got.Status.Provider.Endpoint.Value == workloadName &&
			got.Status.Provider.Endpoint.Port == 50051

		if ready {
			return
		}

		time.Sleep(100 * time.Millisecond)
	}
}
