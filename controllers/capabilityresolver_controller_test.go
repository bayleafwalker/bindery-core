package controllers

import (
	"context"
	"testing"

	"github.com/anvil-platform/anvil/api/v1alpha1"
	"github.com/anvil-platform/anvil/internal/resolver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestStableBindingName_DeterministicAndSafe(t *testing.T) {
	name1 := stableBindingName("anvil-sample-world", "core-interaction-engine", "physics.engine", v1alpha1.CapabilityScopeWorldShard, v1alpha1.MultiplicityOne)
	name2 := stableBindingName("anvil-sample-world", "core-interaction-engine", "physics.engine", v1alpha1.CapabilityScopeWorldShard, v1alpha1.MultiplicityOne)
	if name1 != name2 {
		t.Fatalf("expected deterministic name, got %q vs %q", name1, name2)
	}
	if len(name1) == 0 || len(name1) > 253 {
		t.Fatalf("expected name length 1..253, got %d", len(name1))
	}
	if reNonDNS.MatchString(name1) {
		t.Fatalf("expected DNS-ish name, got %q", name1)
	}
}

func TestCapabilityResolverReconcile_CreatesManagedBinding(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	world := &v1alpha1.WorldInstance{
		TypeMeta: metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "anvil-sample-world", Namespace: "anvil-demo", UID: types.UID("world-uid")},
		Spec: v1alpha1.WorldInstanceSpec{
			GameRef:      v1alpha1.ObjectRef{Name: "anvil-sample"},
			WorldID:      "world-001",
			Region:       "us-test-1",
			ShardCount:   2,
			DesiredState: "Running",
		},
	}

	game := &v1alpha1.GameDefinition{
		TypeMeta: metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "GameDefinition"},
		ObjectMeta: metav1.ObjectMeta{Name: "anvil-sample", Namespace: "anvil-demo"},
		Spec: v1alpha1.GameDefinitionSpec{
			GameID:  "anvil.sample",
			Version: "0.1.0",
			Modules: []v1alpha1.GameModuleRef{
				{Name: "core-physics-engine", Required: true},
				{Name: "core-interaction-engine", Required: true},
			},
		},
	}

	physics := &v1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{Name: "core-physics-engine", Namespace: "anvil-demo"},
		Spec: v1alpha1.ModuleManifestSpec{
			Module: v1alpha1.ModuleIdentity{ID: "core.physics", Version: "1.3.0"},
			Provides: []v1alpha1.ProvidedCapability{
				{CapabilityID: "physics.engine", Version: "1.2.0", Scope: v1alpha1.CapabilityScopeWorldShard, Multiplicity: v1alpha1.MultiplicityOne},
			},
			Requires: []v1alpha1.RequiredCapability{},
			Scaling:  v1alpha1.ModuleScaling{DefaultScope: v1alpha1.CapabilityScopeWorldShard, Statefulness: "stateful"},
		},
	}

	interaction := &v1alpha1.ModuleManifest{
		TypeMeta: metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "ModuleManifest"},
		ObjectMeta: metav1.ObjectMeta{Name: "core-interaction-engine", Namespace: "anvil-demo"},
		Spec: v1alpha1.ModuleManifestSpec{
			Module: v1alpha1.ModuleIdentity{ID: "core.interaction", Version: "0.9.0"},
			Provides: []v1alpha1.ProvidedCapability{
				{CapabilityID: "interaction.engine", Version: "0.9.0", Scope: v1alpha1.CapabilityScopeWorldShard, Multiplicity: v1alpha1.MultiplicityOne},
			},
			Requires: []v1alpha1.RequiredCapability{
				{CapabilityID: "physics.engine", VersionConstraint: "^1.2.0", Scope: v1alpha1.CapabilityScopeWorldShard, Multiplicity: v1alpha1.MultiplicityOne, DependencyMode: v1alpha1.DependencyModeRequired},
			},
			Scaling: v1alpha1.ModuleScaling{DefaultScope: v1alpha1.CapabilityScopeWorldShard, Statefulness: "stateful"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world, game, physics, interaction).Build()

	r := &CapabilityResolverReconciler{Client: cl, Scheme: scheme, Resolver: resolver.NewDefault()}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "anvil-demo", Name: "anvil-sample-world"}})
	if err == nil {
		// continue
	} else {
		t.Fatalf("Reconcile: %v", err)
	}

	bindingName := stableBindingName("anvil-sample-world", "core-interaction-engine", "physics.engine", v1alpha1.CapabilityScopeWorldShard, v1alpha1.MultiplicityOne)
	var binding v1alpha1.CapabilityBinding
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "anvil-demo", Name: bindingName}, &binding); err != nil {
		t.Fatalf("expected binding to be created: %v", err)
	}
	if binding.Spec.Consumer.ModuleManifestName != "core-interaction-engine" {
		t.Fatalf("unexpected consumer: %s", binding.Spec.Consumer.ModuleManifestName)
	}
	if binding.Spec.Provider.ModuleManifestName != "core-physics-engine" {
		t.Fatalf("unexpected provider: %s", binding.Spec.Provider.ModuleManifestName)
	}
	if binding.Labels[labelManagedBy] != managedByCapabilityResolver {
		t.Fatalf("expected managed-by label")
	}
	if binding.Labels[labelWorldName] != "anvil-sample-world" {
		t.Fatalf("expected world label")
	}
	if len(binding.OwnerReferences) != 1 {
		t.Fatalf("expected ownerRef to be set")
	}
}
