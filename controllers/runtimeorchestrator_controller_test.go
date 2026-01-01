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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	binderyv1alpha1 "github.com/bayleafwalker/bindery-core/api/v1alpha1"
)

func TestRuntimeOrchestrator_CreatesServiceDeploymentAndPublishesEndpoint(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(client-go): %v", err)
	}
	if err := binderyv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(game): %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(apps): %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core): %v", err)
	}

	world := &binderyv1alpha1.WorldInstance{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "bindery-sample-world", Namespace: "bindery-demo", UID: types.UID("world-uid")},
		Spec:       binderyv1alpha1.WorldInstanceSpec{GameRef: binderyv1alpha1.ObjectRef{Name: "bindery-sample"}, WorldID: "world-001", Region: "us-test-1", ShardCount: 1},
	}

	provider := &binderyv1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "core-physics-engine",
			Namespace: "bindery-demo",
			Annotations: map[string]string{
				annRuntimeImage: "alpine:3.20",
				annRuntimePort:  "50051",
			},
		},
		Spec: binderyv1alpha1.ModuleManifestSpec{Module: binderyv1alpha1.ModuleIdentity{ID: "core.physics", Version: "1.3.0"}},
	}

	binding := &binderyv1alpha1.CapabilityBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "CapabilityBinding"},
		ObjectMeta: metav1.ObjectMeta{Name: "binding-1", Namespace: "bindery-demo"},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "physics.engine",
			Scope:        binderyv1alpha1.CapabilityScopeWorldShard,
			Multiplicity: binderyv1alpha1.MultiplicityOne,
			WorldRef:     &binderyv1alpha1.WorldRef{Name: "bindery-sample-world"},
			Consumer:     binderyv1alpha1.ConsumerRef{ModuleManifestName: "core-interaction-engine"},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: "core-physics-engine", CapabilityVersion: "1.2.0"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world, provider, binding).WithStatusSubresource(binding, world).Build()

	r := &RuntimeOrchestratorReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "bindery-demo", Name: "binding-1"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	workloadName := rtName(world.Name, provider.Name)

	var svc corev1.Service
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: workloadName}, &svc); err != nil {
		t.Fatalf("expected service: %v", err)
	}

	var dep appsv1.Deployment
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: workloadName}, &dep); err != nil {
		t.Fatalf("expected deployment: %v", err)
	}

	var got binderyv1alpha1.CapabilityBinding
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: "binding-1"}, &got); err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if got.Status.Provider == nil || got.Status.Provider.Endpoint == nil {
		t.Fatalf("expected endpoint to be set")
	}
	if got.Status.Provider.Endpoint.Type != "kubernetesService" || got.Status.Provider.Endpoint.Value != workloadName {
		t.Fatalf("unexpected endpoint: %#v", got.Status.Provider.Endpoint)
	}
}

func TestRuntimeOrchestrator_ShardLabeledBindingCreatesShardWorkloadName(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(client-go): %v", err)
	}
	if err := binderyv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(game): %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(apps): %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core): %v", err)
	}

	world := &binderyv1alpha1.WorldInstance{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "bindery-sample-world", Namespace: "bindery-demo", UID: types.UID("world-uid")},
		Spec:       binderyv1alpha1.WorldInstanceSpec{GameRef: binderyv1alpha1.ObjectRef{Name: "bindery-sample"}, WorldID: "world-001", Region: "us-test-1", ShardCount: 2},
	}

	shardName := stableWorldShardName(world.Name, 0)
	shard := &binderyv1alpha1.WorldShard{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldShard"},
		ObjectMeta: metav1.ObjectMeta{Name: shardName, Namespace: "bindery-demo", Labels: map[string]string{labelWorldName: world.Name}},
		Spec:       binderyv1alpha1.WorldShardSpec{WorldRef: binderyv1alpha1.ObjectRef{Name: world.Name}, ShardID: 0},
	}

	provider := &binderyv1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "core-physics-engine",
			Namespace: "bindery-demo",
			Annotations: map[string]string{
				annRuntimeImage: "alpine:3.20",
				annRuntimePort:  "50051",
			},
		},
		Spec: binderyv1alpha1.ModuleManifestSpec{Module: binderyv1alpha1.ModuleIdentity{ID: "core.physics", Version: "1.3.0"}},
	}

	binding := &binderyv1alpha1.CapabilityBinding{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "CapabilityBinding"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "binding-1",
			Namespace: "bindery-demo",
			Labels: map[string]string{
				labelShardID: "0",
			},
		},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "physics.engine",
			Scope:        binderyv1alpha1.CapabilityScopeWorldShard,
			Multiplicity: binderyv1alpha1.MultiplicityOne,
			WorldRef:     &binderyv1alpha1.WorldRef{Name: world.Name},
			Consumer:     binderyv1alpha1.ConsumerRef{ModuleManifestName: "core-interaction-engine"},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: provider.Name, CapabilityVersion: "1.2.0"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world, shard, provider, binding).WithStatusSubresource(binding, world).Build()

	r := &RuntimeOrchestratorReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "bindery-demo", Name: binding.Name}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	workloadName := rtNameWithShard(world.Name, "0", provider.Name)

	var svc corev1.Service
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: workloadName}, &svc); err != nil {
		t.Fatalf("expected service: %v", err)
	}

	var dep appsv1.Deployment
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: workloadName}, &dep); err != nil {
		t.Fatalf("expected deployment: %v", err)
	}

	var got binderyv1alpha1.CapabilityBinding
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: binding.Name}, &got); err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if got.Status.Provider == nil || got.Status.Provider.Endpoint == nil {
		t.Fatalf("expected endpoint to be set")
	}
	if got.Status.Provider.Endpoint.Value != workloadName {
		t.Fatalf("expected endpoint to reference shard workload name")
	}
}

func TestRuntimeOrchestrator_ServerStorageCreatesClaimAndMount(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(client-go): %v", err)
	}
	if err := binderyv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(game): %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(apps): %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core): %v", err)
	}

	world := &binderyv1alpha1.WorldInstance{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "bindery-sample-world", Namespace: "bindery-demo", UID: types.UID("world-uid")},
		Spec:       binderyv1alpha1.WorldInstanceSpec{GameRef: binderyv1alpha1.ObjectRef{Name: "bindery-sample"}, WorldID: "world-001", Region: "us-test-1", ShardCount: 2},
	}

	shardName := stableWorldShardName(world.Name, 0)
	shard := &binderyv1alpha1.WorldShard{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldShard"},
		ObjectMeta: metav1.ObjectMeta{Name: shardName, Namespace: "bindery-demo", Labels: map[string]string{labelWorldName: world.Name}},
		Spec:       binderyv1alpha1.WorldShardSpec{WorldRef: binderyv1alpha1.ObjectRef{Name: world.Name}, ShardID: 0},
	}

	provider := &binderyv1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "core-physics-engine",
			Namespace: "bindery-demo",
			Annotations: map[string]string{
				annRuntimeImage:       "alpine:3.20",
				annRuntimePort:        "50051",
				annStorageTier:        "server-low-latency",
				annStorageScope:       "world-shard",
				annStorageSize:        "2Gi",
				annStorageMountPath:   "/data",
				annStorageAccessModes: "ReadWriteOnce",
			},
		},
		Spec: binderyv1alpha1.ModuleManifestSpec{Module: binderyv1alpha1.ModuleIdentity{ID: "core.physics", Version: "1.3.0"}},
	}

	binding := &binderyv1alpha1.CapabilityBinding{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "CapabilityBinding"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "binding-1",
			Namespace: "bindery-demo",
			Labels: map[string]string{
				labelShardID: "0",
			},
		},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "physics.engine",
			Scope:        binderyv1alpha1.CapabilityScopeWorldShard,
			Multiplicity: binderyv1alpha1.MultiplicityOne,
			WorldRef:     &binderyv1alpha1.WorldRef{Name: world.Name},
			Consumer:     binderyv1alpha1.ConsumerRef{ModuleManifestName: "core-interaction-engine"},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: provider.Name, CapabilityVersion: "1.2.0"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world, shard, provider, binding).WithStatusSubresource(binding, world).Build()

	r := &RuntimeOrchestratorReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "bindery-demo", Name: binding.Name}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	claimName := stableWSCName(world.Name, shardName, "server-low-latency")
	var claim binderyv1alpha1.WorldStorageClaim
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: claimName}, &claim); err != nil {
		t.Fatalf("expected WorldStorageClaim: %v", err)
	}
	if claim.Spec.Scope != binderyv1alpha1.WorldStorageScopeWorldShard {
		t.Fatalf("unexpected claim scope: %s", claim.Spec.Scope)
	}
	if claim.Spec.ShardRef == nil || claim.Spec.ShardRef.Name != shardName {
		t.Fatalf("expected shardRef to be set")
	}

	workloadName := rtNameWithShard(world.Name, "0", provider.Name)
	var dep appsv1.Deployment
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: workloadName}, &dep); err != nil {
		t.Fatalf("expected deployment: %v", err)
	}

	expectedPVC := stablePVCName(world.Name, shardName, "server-low-latency")
	foundVolume := false
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "bindery-state" && v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == expectedPVC {
			foundVolume = true
			break
		}
	}
	if !foundVolume {
		t.Fatalf("expected bindery-state volume with pvc %q", expectedPVC)
	}

	if len(dep.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container")
	}
	foundMount := false
	for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
		if m.Name == "bindery-state" && m.MountPath == "/data" {
			foundMount = true
			break
		}
	}
	if !foundMount {
		t.Fatalf("expected bindery-state mount at /data")
	}
}

func TestRuntimeOrchestrator_SkipsWhenNoWorldRef(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(client-go): %v", err)
	}
	if err := binderyv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(game): %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(apps): %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core): %v", err)
	}

	binding := &binderyv1alpha1.CapabilityBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "CapabilityBinding"},
		ObjectMeta: metav1.ObjectMeta{Name: "binding-1", Namespace: "bindery-demo"},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "physics.engine",
			Scope:        binderyv1alpha1.CapabilityScopeWorldShard,
			Multiplicity: binderyv1alpha1.MultiplicityOne,
			Consumer:     binderyv1alpha1.ConsumerRef{ModuleManifestName: "core-interaction-engine"},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: "core-physics-engine", CapabilityVersion: "1.2.0"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(binding).WithStatusSubresource(binding).Build()

	r := &RuntimeOrchestratorReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "bindery-demo", Name: "binding-1"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got binderyv1alpha1.CapabilityBinding
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: "binding-1"}, &got); err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if got.Status.Provider != nil {
		t.Fatalf("expected provider status to remain unset")
	}
}

func TestRuntimeOrchestrator_SkipsWhenNoRuntimeImageAnnotation(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(client-go): %v", err)
	}
	if err := binderyv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(game): %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(apps): %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core): %v", err)
	}

	world := &binderyv1alpha1.WorldInstance{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "bindery-sample-world", Namespace: "bindery-demo", UID: types.UID("world-uid")},
		Spec:       binderyv1alpha1.WorldInstanceSpec{GameRef: binderyv1alpha1.ObjectRef{Name: "bindery-sample"}, WorldID: "world-001", Region: "us-test-1", ShardCount: 1},
	}

	provider := &binderyv1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "core-physics-engine",
			Namespace: "bindery-demo",
		},
		Spec: binderyv1alpha1.ModuleManifestSpec{Module: binderyv1alpha1.ModuleIdentity{ID: "core.physics", Version: "1.3.0"}},
	}

	binding := &binderyv1alpha1.CapabilityBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "CapabilityBinding"},
		ObjectMeta: metav1.ObjectMeta{Name: "binding-1", Namespace: "bindery-demo"},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "physics.engine",
			Scope:        binderyv1alpha1.CapabilityScopeWorldShard,
			Multiplicity: binderyv1alpha1.MultiplicityOne,
			WorldRef:     &binderyv1alpha1.WorldRef{Name: "bindery-sample-world"},
			Consumer:     binderyv1alpha1.ConsumerRef{ModuleManifestName: "core-interaction-engine"},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: "core-physics-engine", CapabilityVersion: "1.2.0"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world, provider, binding).WithStatusSubresource(binding).Build()

	r := &RuntimeOrchestratorReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "bindery-demo", Name: "binding-1"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	workloadName := rtName(world.Name, provider.Name)
	var svc corev1.Service
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: workloadName}, &svc); err == nil {
		t.Fatalf("expected no service to be created")
	}

	var got binderyv1alpha1.CapabilityBinding
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: "binding-1"}, &got); err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if got.Status.Provider != nil {
		t.Fatalf("expected provider status to remain unset")
	}
}

func TestRuntimeOrchestrator_InvalidRuntimePortFallsBackToDefault(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(client-go): %v", err)
	}
	if err := binderyv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(game): %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(apps): %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core): %v", err)
	}

	world := &binderyv1alpha1.WorldInstance{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "bindery-sample-world", Namespace: "bindery-demo", UID: types.UID("world-uid")},
		Spec:       binderyv1alpha1.WorldInstanceSpec{GameRef: binderyv1alpha1.ObjectRef{Name: "bindery-sample"}, WorldID: "world-001", Region: "us-test-1", ShardCount: 1},
	}

	provider := &binderyv1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "core-physics-engine",
			Namespace: "bindery-demo",
			Annotations: map[string]string{
				annRuntimeImage: "alpine:3.20",
				annRuntimePort:  "not-a-number",
			},
		},
		Spec: binderyv1alpha1.ModuleManifestSpec{Module: binderyv1alpha1.ModuleIdentity{ID: "core.physics", Version: "1.3.0"}},
	}

	binding := &binderyv1alpha1.CapabilityBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "CapabilityBinding"},
		ObjectMeta: metav1.ObjectMeta{Name: "binding-1", Namespace: "bindery-demo"},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "physics.engine",
			Scope:        binderyv1alpha1.CapabilityScopeWorldShard,
			Multiplicity: binderyv1alpha1.MultiplicityOne,
			WorldRef:     &binderyv1alpha1.WorldRef{Name: "bindery-sample-world"},
			Consumer:     binderyv1alpha1.ConsumerRef{ModuleManifestName: "core-interaction-engine"},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: "core-physics-engine", CapabilityVersion: "1.2.0"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world, provider, binding).WithStatusSubresource(binding, world).Build()

	r := &RuntimeOrchestratorReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "bindery-demo", Name: "binding-1"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got binderyv1alpha1.CapabilityBinding
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "bindery-demo", Name: "binding-1"}, &got); err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if got.Status.Provider == nil || got.Status.Provider.Endpoint == nil {
		t.Fatalf("expected endpoint to be set")
	}
	if got.Status.Provider.Endpoint.Port != 50051 {
		t.Fatalf("expected default port 50051, got %d", got.Status.Provider.Endpoint.Port)
	}
}

func TestRuntimeOrchestrator_InjectsDependencyEndpoints(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = binderyv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	world := &binderyv1alpha1.WorldInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"},
	}

	// Physics Module (Provider)
	physicsMM := &binderyv1alpha1.ModuleManifest{
		ObjectMeta: metav1.ObjectMeta{Name: "physics-mod", Namespace: "default"},
	}

	// Game Module (Consumer)
	gameMM := &binderyv1alpha1.ModuleManifest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "game-mod",
			Namespace: "default",
			Annotations: map[string]string{
				annRuntimeImage: "game:latest",
			},
		},
	}

	// Binding 1: Dependency (Consumer=Game, Provider=Physics)
	// This binding represents the resolved dependency. It has the endpoint.
	bindingDep := &binderyv1alpha1.CapabilityBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "binding-dep", Namespace: "default"},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "physics.engine",
			WorldRef:     &binderyv1alpha1.WorldRef{Name: "world-1"},
			Consumer:     binderyv1alpha1.ConsumerRef{ModuleManifestName: "game-mod"},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: "physics-mod"},
		},
		Status: binderyv1alpha1.CapabilityBindingStatus{
			Provider: &binderyv1alpha1.ProviderStatus{
				Endpoint: &binderyv1alpha1.EndpointRef{
					Type:  "kubernetesService",
					Value: "physics-svc",
					Port:  8080,
				},
			},
		},
	}

	// Binding 2: Game Deployment (Provider=Game)
	// This is the binding we reconcile to deploy Game.
	bindingGame := &binderyv1alpha1.CapabilityBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "binding-game", Namespace: "default"},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "game.logic",
			WorldRef:     &binderyv1alpha1.WorldRef{Name: "world-1"},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: "game-mod"},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&binderyv1alpha1.CapabilityBinding{}, idxBindingConsumer, func(rawObj client.Object) []string {
			binding := rawObj.(*binderyv1alpha1.CapabilityBinding)
			if binding.Spec.Consumer.ModuleManifestName == "" {
				return nil
			}
			return []string{binding.Spec.Consumer.ModuleManifestName}
		}).
		WithObjects(world, physicsMM, gameMM, bindingDep, bindingGame).
		WithStatusSubresource(bindingGame, world).
		Build()

	r := &RuntimeOrchestratorReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "binding-game"}})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Check Deployment
	var dep appsv1.Deployment
	// Name is rtName(world, provider) -> "rt-world-1-game-mod"
	depName := rtName("world-1", "game-mod")
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "default", Name: depName}, &dep); err != nil {
		t.Fatalf("Deployment not found: %v", err)
	}

	// Check Env Vars
	found := false
	for _, env := range dep.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "BINDERY_CAPABILITY_PHYSICS_ENGINE_ENDPOINT" {
			if env.Value != "physics-svc:8080" {
				t.Errorf("Expected BINDERY_CAPABILITY_PHYSICS_ENGINE_ENDPOINT=physics-svc:8080, got %s", env.Value)
			}
			found = true
		}
	}
	if !found {
		t.Error("BINDERY_CAPABILITY_PHYSICS_ENGINE_ENDPOINT env var not found")
	}
}

func TestRuntimeOrchestrator_GracefulTermination(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = binderyv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	world := &binderyv1alpha1.WorldInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"},
		Spec:       binderyv1alpha1.WorldInstanceSpec{WorldID: "w1", ShardCount: 1},
	}

	provider := &binderyv1alpha1.ModuleManifest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "provider-mod",
			Namespace: "default",
			Annotations: map[string]string{
				annRuntimeImage:           "img",
				annTerminationGracePeriod: "60",
				annPreStopCommand:         "/bin/sleep 10",
			},
		},
	}

	binding := &binderyv1alpha1.CapabilityBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "binding-1", Namespace: "default"},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			WorldRef: &binderyv1alpha1.WorldRef{Name: "world-1"},
			Provider: binderyv1alpha1.ProviderRef{ModuleManifestName: "provider-mod"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world, provider, binding).WithStatusSubresource(binding, world).Build()
	r := &RuntimeOrchestratorReconciler{Client: cl, Scheme: scheme}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "binding-1"}})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	var dep appsv1.Deployment
	// Name is rtName(world.Name, provider.Name) -> "rt-world-1-provider-mod"
	depName := "rt-world-1-provider-mod"
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "default", Name: depName}, &dep); err != nil {
		t.Fatalf("Deployment not found: %v", err)
	}

	if dep.Spec.Template.Spec.TerminationGracePeriodSeconds == nil || *dep.Spec.Template.Spec.TerminationGracePeriodSeconds != 60 {
		t.Errorf("Expected TerminationGracePeriodSeconds 60, got %v", dep.Spec.Template.Spec.TerminationGracePeriodSeconds)
	}

	container := dep.Spec.Template.Spec.Containers[0]
	if container.Lifecycle == nil || container.Lifecycle.PreStop == nil || container.Lifecycle.PreStop.Exec == nil {
		t.Fatal("Expected PreStop hook")
	}
	cmd := container.Lifecycle.PreStop.Exec.Command
	if len(cmd) != 3 || cmd[2] != "/bin/sleep 10" {
		t.Errorf("Unexpected PreStop command: %v", cmd)
	}
}
