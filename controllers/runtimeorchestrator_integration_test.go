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
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	binderyv1alpha1 "github.com/bayleafwalker/bindery-core/api/v1alpha1"
)

func TestIntegration_RuntimeOrchestrator_PublishesEndpointToStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short")
	}
	if os.Getenv("BINDERY_INTEGRATION") != "1" {
		t.Skip("set BINDERY_INTEGRATION=1 (or run `make test-integration`) to enable envtest integration tests")
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
	if err := binderyv1alpha1.AddToScheme(scheme); err != nil {
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

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "bindery-integration"}}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	world := &binderyv1alpha1.WorldInstance{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: ns.Name},
		Spec: binderyv1alpha1.WorldInstanceSpec{
			GameRef:      binderyv1alpha1.ObjectRef{Name: "game-1"},
			WorldID:      "world-001",
			Region:       "test-1",
			ShardCount:   1,
			DesiredState: "Running",
		},
	}
	if err := k8sClient.Create(ctx, world); err != nil {
		t.Fatalf("create world: %v", err)
	}

	provider := &binderyv1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "core-physics-engine",
			Namespace: ns.Name,
			Annotations: map[string]string{
				annRuntimeImage: "alpine:3.20",
				annRuntimePort:  "50051",
			},
		},
		Spec: binderyv1alpha1.ModuleManifestSpec{
			Module: binderyv1alpha1.ModuleIdentity{ID: "core.physics", Version: "1.3.0"},
			Provides: []binderyv1alpha1.ProvidedCapability{{
				CapabilityID: "physics.engine",
				Version:      "1.3.0",
				Scope:        binderyv1alpha1.CapabilityScopeWorldShard,
				Multiplicity: binderyv1alpha1.MultiplicityOne,
			}},
			Requires: []binderyv1alpha1.RequiredCapability{},
			Scaling:  binderyv1alpha1.ModuleScaling{DefaultScope: binderyv1alpha1.CapabilityScopeWorldShard, Statefulness: "stateless"},
		},
	}
	if err := k8sClient.Create(ctx, provider); err != nil {
		t.Fatalf("create provider modulemanifest: %v", err)
	}

	binding := &binderyv1alpha1.CapabilityBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "CapabilityBinding"},
		ObjectMeta: metav1.ObjectMeta{Name: "binding-1", Namespace: ns.Name},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "physics.engine",
			Scope:        binderyv1alpha1.CapabilityScopeWorldShard,
			Multiplicity: binderyv1alpha1.MultiplicityOne,
			WorldRef:     &binderyv1alpha1.WorldRef{Name: world.Name},
			Consumer:     binderyv1alpha1.ConsumerRef{ModuleManifestName: "core-interaction-engine"},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: provider.Name, CapabilityVersion: "1.3.0"},
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

	if err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		var svc corev1.Service
		svcErr := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, &svc)

		var dep appsv1.Deployment
		depErr := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, &dep)

		var got binderyv1alpha1.CapabilityBinding
		bindErr := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: binding.Name}, &got)

		ready := svcErr == nil && depErr == nil && bindErr == nil &&
			got.Status.Provider != nil && got.Status.Provider.Endpoint != nil &&
			got.Status.Provider.Endpoint.Type == "kubernetesService" &&
			got.Status.Provider.Endpoint.Value == workloadName &&
			got.Status.Provider.Endpoint.Port == 50051

		return ready, nil
	}); err != nil {
		t.Fatalf("timed out waiting for runtime resources + status: %v", err)
	}
}

func TestIntegration_RuntimeOrchestrator_ShardStorage_CreatesClaimMountAndPVC(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short")
	}
	if os.Getenv("BINDERY_INTEGRATION") != "1" {
		t.Skip("set BINDERY_INTEGRATION=1 (or run `make test-integration`) to enable envtest integration tests")
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
		t.Fatalf("start envtest (set KUBEBUILDER_ASSETS; try `make test-integration`): %v", err)
	}
	defer func() {
		_ = testEnv.Stop()
	}()

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

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "bindery-integration"}}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	world := &binderyv1alpha1.WorldInstance{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: ns.Name},
		Spec: binderyv1alpha1.WorldInstanceSpec{
			GameRef:      binderyv1alpha1.ObjectRef{Name: "game-1"},
			WorldID:      "world-001",
			Region:       "test-1",
			ShardCount:   2,
			DesiredState: "Running",
		},
	}
	if err := k8sClient.Create(ctx, world); err != nil {
		t.Fatalf("create world: %v", err)
	}

	shardName := stableWorldShardName(world.Name, 0)
	shard := &binderyv1alpha1.WorldShard{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldShard"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      shardName,
			Namespace: ns.Name,
			Labels: map[string]string{
				labelWorldName: world.Name,
			},
		},
		Spec: binderyv1alpha1.WorldShardSpec{WorldRef: binderyv1alpha1.ObjectRef{Name: world.Name}, ShardID: 0},
	}
	if err := k8sClient.Create(ctx, shard); err != nil {
		t.Fatalf("create shard: %v", err)
	}

	provider := &binderyv1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "core-physics-engine",
			Namespace: ns.Name,
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
		Spec: binderyv1alpha1.ModuleManifestSpec{
			Module: binderyv1alpha1.ModuleIdentity{ID: "core.physics", Version: "1.3.0"},
			Provides: []binderyv1alpha1.ProvidedCapability{{
				CapabilityID: "physics.engine",
				Version:      "1.3.0",
				Scope:        binderyv1alpha1.CapabilityScopeWorldShard,
				Multiplicity: binderyv1alpha1.MultiplicityOne,
			}},
			Requires: []binderyv1alpha1.RequiredCapability{},
			Scaling:  binderyv1alpha1.ModuleScaling{DefaultScope: binderyv1alpha1.CapabilityScopeWorldShard, Statefulness: "stateful"},
		},
	}
	if err := k8sClient.Create(ctx, provider); err != nil {
		t.Fatalf("create provider modulemanifest: %v", err)
	}

	binding := &binderyv1alpha1.CapabilityBinding{
		TypeMeta: metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "CapabilityBinding"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "binding-1",
			Namespace: ns.Name,
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
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: provider.Name, CapabilityVersion: "1.3.0"},
		},
	}
	if err := k8sClient.Create(ctx, binding); err != nil {
		t.Fatalf("create binding: %v", err)
	}

	rt := &RuntimeOrchestratorReconciler{Client: k8sClient, Scheme: scheme}
	if _, err := rt.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns.Name, Name: binding.Name}}); err != nil {
		t.Fatalf("runtime reconcile: %v", err)
	}

	workloadName := rtNameWithShard(world.Name, "0", provider.Name)
	claimName := stableWSCName(world.Name, shardName, "server-low-latency")
	expectedPVC := stablePVCName(world.Name, shardName, "server-low-latency")

	// Wait for the runtime reconciler's writes to be observable.
	if err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		var claim binderyv1alpha1.WorldStorageClaim
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: claimName}, &claim); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		if claim.Spec.Scope != binderyv1alpha1.WorldStorageScopeWorldShard {
			return false, nil
		}
		if claim.Spec.ShardRef == nil || claim.Spec.ShardRef.Name != shardName {
			return false, nil
		}

		var dep appsv1.Deployment
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workloadName}, &dep); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		foundVol := false
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == "bindery-state" && v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == expectedPVC {
				foundVol = true
				break
			}
		}
		if !foundVol {
			return false, nil
		}
		if len(dep.Spec.Template.Spec.Containers) != 1 {
			return false, nil
		}
		foundMount := false
		for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
			if m.Name == "bindery-state" && m.MountPath == "/data" {
				foundMount = true
				break
			}
		}
		if !foundMount {
			return false, nil
		}

		var got binderyv1alpha1.CapabilityBinding
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: binding.Name}, &got); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		if got.Status.Provider == nil || got.Status.Provider.Endpoint == nil {
			return false, nil
		}
		if got.Status.Provider.Endpoint.Value != workloadName {
			return false, nil
		}
		return true, nil
	}); err != nil {
		t.Fatalf("wait for claim+deployment+status: %v", err)
	}

	// Now reconcile the StorageOrchestrator and verify the PVC is created.
	so := &StorageOrchestratorReconciler{Client: k8sClient, Scheme: scheme}
	if _, err := so.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns.Name, Name: claimName}}); err != nil {
		t.Fatalf("storage reconcile: %v", err)
	}
	if err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		var pvc corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: expectedPVC}, &pvc); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return true, nil
	}); err != nil {
		t.Fatalf("wait for pvc: %v", err)
	}
}

func TestIntegration_RuntimeOrchestrator_InjectsEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short")
	}
	if os.Getenv("BINDERY_INTEGRATION") != "1" {
		t.Skip("set BINDERY_INTEGRATION=1 (or run `make test-integration`) to enable envtest integration tests")
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
		t.Fatalf("start envtest: %v", err)
	}
	defer func() { _ = testEnv.Stop() }()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = binderyv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	// Setup Manager to run the controller (needed for Watches/Indexing)
	// Only register the controller once per test run to avoid duplicate errors
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme, Metrics: metricsserver.Options{BindAddress: "0"}})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := (&RuntimeOrchestratorReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("runtimeorchestrator"),
	}).SetupWithManager(mgr); err != nil {
		t.Fatalf("setup with manager: %v", err)
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			// t.Logf("manager stopped: %v", err)
		}
	}()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "bindery-integration-inject"}}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	world := &binderyv1alpha1.WorldInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: ns.Name},
		Spec: binderyv1alpha1.WorldInstanceSpec{
			GameRef: binderyv1alpha1.ObjectRef{Name: "game-1"},
			WorldID: "world-001",
		},
	}
	if err := k8sClient.Create(ctx, world); err != nil {
		t.Fatalf("create world: %v", err)
	}

	// 1. Create Provider (Physics)
	physicsMM := &binderyv1alpha1.ModuleManifest{
		ObjectMeta: metav1.ObjectMeta{Name: "physics-mod", Namespace: ns.Name},
		Spec: binderyv1alpha1.ModuleManifestSpec{
			Module: binderyv1alpha1.ModuleIdentity{ID: "core.physics", Version: "1.0.0"},
		},
	}
	if err := k8sClient.Create(ctx, physicsMM); err != nil {
		t.Fatalf("create physics MM: %v", err)
	}

	// 2. Create Consumer (Game)
	gameMM := &binderyv1alpha1.ModuleManifest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "game-mod",
			Namespace: ns.Name,
			Annotations: map[string]string{
				annRuntimeImage: "game:latest",
			},
		},
		Spec: binderyv1alpha1.ModuleManifestSpec{
			Module: binderyv1alpha1.ModuleIdentity{ID: "game.logic", Version: "1.0.0"},
		},
	}
	if err := k8sClient.Create(ctx, gameMM); err != nil {
		t.Fatalf("create game MM: %v", err)
	}

	// 3. Create Binding for Physics (Dependency)
	// Initially NO endpoint.
	bindingDep := &binderyv1alpha1.CapabilityBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "binding-dep", Namespace: ns.Name},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "physics.engine",
			WorldRef:     &binderyv1alpha1.WorldRef{Name: world.Name},
			Consumer:     binderyv1alpha1.ConsumerRef{ModuleManifestName: "game-mod"},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: "physics-mod"},
		},
	}
	if err := k8sClient.Create(ctx, bindingDep); err != nil {
		t.Fatalf("create binding dep: %v", err)
	}

	// 4. Create Binding for Game (Consumer)
	bindingGame := &binderyv1alpha1.CapabilityBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "binding-game", Namespace: ns.Name},
		Spec: binderyv1alpha1.CapabilityBindingSpec{
			CapabilityID: "game.logic",
			WorldRef:     &binderyv1alpha1.WorldRef{Name: world.Name},
			Provider:     binderyv1alpha1.ProviderRef{ModuleManifestName: "game-mod"},
		},
	}
	if err := k8sClient.Create(ctx, bindingGame); err != nil {
		t.Fatalf("create binding game: %v", err)
	}

	// Wait for Game Deployment to be created (initially without env var)
	depName := rtName(world.Name, "game-mod")
	if err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		var dep appsv1.Deployment
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: depName}, &dep); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return true, nil
	}); err != nil {
		t.Fatalf("wait for game deployment: %v", err)
	}

	// 5. Update Physics Binding with Endpoint
	// This should trigger the watch -> reconcile Game -> update Deployment
	bindingDep.Status.Provider = &binderyv1alpha1.ProviderStatus{
		Endpoint: &binderyv1alpha1.EndpointRef{
			Type:  "kubernetesService",
			Value: "physics-svc",
			Port:  8080,
		},
	}
	if err := k8sClient.Status().Update(ctx, bindingDep); err != nil {
		t.Fatalf("update binding dep status: %v", err)
	}

	// 6. Verify Game Deployment gets the Env Var
	if err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		var dep appsv1.Deployment
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: depName}, &dep); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		for _, env := range dep.Spec.Template.Spec.Containers[0].Env {
			if env.Name == "BINDERY_CAPABILITY_PHYSICS_ENGINE_ENDPOINT" && env.Value == "physics-svc:8080" {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("timed out waiting for env var injection: %v", err)
	}
}
