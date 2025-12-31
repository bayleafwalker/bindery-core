package controllers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
	"github.com/anvil-platform/anvil/internal/resolver"
)

func TestIntegration_RealmArchitecture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short")
	}
	if os.Getenv("ANVIL_INTEGRATION") != "1" {
		t.Skip("set ANVIL_INTEGRATION=1 to enable envtest integration tests")
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
	_ = gamev1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Register all controllers
	if err := (&CapabilityResolverReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Resolver: resolver.NewDefault(),
		Recorder: mgr.GetEventRecorderFor("CapabilityResolver"),
	}).SetupWithManager(mgr); err != nil {
		t.Fatalf("setup resolver: %v", err)
	}

	if err := (&RuntimeOrchestratorReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("RuntimeOrchestrator"),
	}).SetupWithManager(mgr); err != nil {
		t.Fatalf("setup orchestrator: %v", err)
	}

	if err := (&RealmReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("Realm"),
	}).SetupWithManager(mgr); err != nil {
		t.Fatalf("setup realm: %v", err)
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			// t.Logf("manager stopped: %v", err)
		}
	}()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "anvil-realm-test"}}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	// 1. Define Modules
	// Global Chat Module
	chatMM := &gamev1alpha1.ModuleManifest{
		ObjectMeta: metav1.ObjectMeta{Name: "global-chat", Namespace: ns.Name, Annotations: map[string]string{
			"anvil.dev/runtime-image": "chat:latest",
		}},
		Spec: gamev1alpha1.ModuleManifestSpec{
			Module: gamev1alpha1.ModuleIdentity{ID: "svc.chat", Version: "1.0.0"},
			Provides: []gamev1alpha1.ProvidedCapability{{
				CapabilityID: "system.chat",
				Version:      "1.0.0",
				Scope:        gamev1alpha1.CapabilityScopeRealm,
				Multiplicity: gamev1alpha1.MultiplicityOne,
			}},
			Requires: []gamev1alpha1.RequiredCapability{},
			Scaling: gamev1alpha1.ModuleScaling{
				Statefulness: "stateful",
				DefaultScope: gamev1alpha1.CapabilityScopeRealm,
			},
		},
	}
	if err := k8sClient.Create(ctx, chatMM); err != nil {
		t.Fatalf("create chat mm: %v", err)
	}

	// Game Server Module (Requires Chat)
	gameMM := &gamev1alpha1.ModuleManifest{
		ObjectMeta: metav1.ObjectMeta{Name: "game-server", Namespace: ns.Name, Annotations: map[string]string{
			"anvil.dev/runtime-image": "game:latest",
		}},
		Spec: gamev1alpha1.ModuleManifestSpec{
			Module:   gamev1alpha1.ModuleIdentity{ID: "svc.game", Version: "1.0.0"},
			Provides: []gamev1alpha1.ProvidedCapability{},
			Requires: []gamev1alpha1.RequiredCapability{{
				CapabilityID:      "system.chat",
				VersionConstraint: "*",
				Scope:             gamev1alpha1.CapabilityScopeRealm,
				DependencyMode:    gamev1alpha1.DependencyModeRequired,
				Multiplicity:      gamev1alpha1.MultiplicityOne,
			}},
			Scaling: gamev1alpha1.ModuleScaling{
				Statefulness: "stateless",
				DefaultScope: gamev1alpha1.CapabilityScopeWorld,
			},
		},
	}
	if err := k8sClient.Create(ctx, gameMM); err != nil {
		t.Fatalf("create game mm: %v", err)
	}

	// 2. Create Realm
	realm := &gamev1alpha1.Realm{
		ObjectMeta: metav1.ObjectMeta{Name: "eu-west", Namespace: ns.Name},
		Spec: gamev1alpha1.RealmSpec{
			Modules: []gamev1alpha1.RealmModule{{Name: "global-chat"}},
		},
	}
	if err := k8sClient.Create(ctx, realm); err != nil {
		t.Fatalf("create realm: %v", err)
	}

	// 3. Create GameDefinition
	gameDef := &gamev1alpha1.GameDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "mmo-game", Namespace: ns.Name},
		Spec: gamev1alpha1.GameDefinitionSpec{
			GameID:  "mmo-game",
			Version: "1.0.0",
			Modules: []gamev1alpha1.GameModuleRef{{Name: "game-server"}},
		},
	}
	if err := k8sClient.Create(ctx, gameDef); err != nil {
		t.Fatalf("create game def: %v", err)
	}

	// 4. Create WorldInstance linked to Realm
	world := &gamev1alpha1.WorldInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "world-alpha", Namespace: ns.Name},
		Spec: gamev1alpha1.WorldInstanceSpec{
			GameRef:    gamev1alpha1.ObjectRef{Name: "mmo-game"},
			RealmRef:   &gamev1alpha1.ObjectRef{Name: "eu-west"},
			WorldID:    "w-1",
			Region:     "eu-west-1",
			ShardCount: 1,
		},
	}
	if err := k8sClient.Create(ctx, world); err != nil {
		t.Fatalf("create world: %v", err)
	}

	// 5. Verify Realm Binding (created by RealmController)
	t.Log("Waiting for Realm Binding...")
	realmBindingName := "realm-eu-west-global-chat"
	if err := waitForBinding(ctx, k8sClient, types.NamespacedName{Namespace: ns.Name, Name: realmBindingName}); err != nil {
		t.Fatalf("realm binding not found: %v", err)
	}

	// 6. Verify World Binding (created by CapabilityResolver)
	// Should bind game-server -> global-chat
	t.Log("Waiting for World Binding...")
	// Name is deterministic but complex, let's list bindings for the world
	var worldBindings gamev1alpha1.CapabilityBindingList
	if err := waitForCondition(ctx, func() (bool, error) {
		if err := k8sClient.List(ctx, &worldBindings, client.InNamespace(ns.Name), client.MatchingLabels{"game.platform/world": "world-alpha"}); err != nil {
			return false, err
		}
		for _, b := range worldBindings.Items {
			if b.Spec.CapabilityID == "system.chat" && b.Spec.Provider.ModuleManifestName == "global-chat" {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("world binding for chat not found: %v", err)
	}

	// 7. Verify Deployments
	// Realm Deployment
	realmDepName := "rt-global-global-chat" // "global" is the synthetic world name for realm bindings
	if err := waitForDeployment(ctx, k8sClient, types.NamespacedName{Namespace: ns.Name, Name: realmDepName}); err != nil {
		t.Fatalf("realm deployment not found: %v", err)
	}

	// World Deployment
	worldDepName := "rt-world-alpha-game-server"
	if err := waitForDeployment(ctx, k8sClient, types.NamespacedName{Namespace: ns.Name, Name: worldDepName}); err != nil {
		t.Fatalf("world deployment not found: %v", err)
	}
}

func waitForBinding(ctx context.Context, c client.Client, nn types.NamespacedName) error {
	return waitForCondition(ctx, func() (bool, error) {
		var b gamev1alpha1.CapabilityBinding
		err := c.Get(ctx, nn, &b)
		if err == nil {
			return true, nil
		}
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	})
}

func waitForDeployment(ctx context.Context, c client.Client, nn types.NamespacedName) error {
	return waitForCondition(ctx, func() (bool, error) {
		var d appsv1.Deployment
		err := c.Get(ctx, nn, &d)
		if err == nil {
			return true, nil
		}
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	})
}

func waitForCondition(ctx context.Context, check func() (bool, error)) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ok, err := check()
			if err != nil {
				return err
			}
			if ok {
				return nil
			}
		}
	}
}
