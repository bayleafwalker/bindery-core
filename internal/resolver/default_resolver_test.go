package resolver

import (
	"context"
	"testing"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mm(name string, provides []gamev1alpha1.ProvidedCapability, requires []gamev1alpha1.RequiredCapability) gamev1alpha1.ModuleManifest {
	return gamev1alpha1.ModuleManifest{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: gamev1alpha1.ModuleManifestSpec{
			Module:   gamev1alpha1.ModuleIdentity{ID: name, Version: "0.0.0"},
			Provides: provides,
			Requires: requires,
		},
	}
}

func TestDefaultResolver_SelectsHighestCompatibleProvider(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: gamev1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []gamev1alpha1.ModuleManifest{
			mm("physics-a", []gamev1alpha1.ProvidedCapability{{
				CapabilityID: "cap.physics",
				Version:      "1.0.0",
				Scope:        gamev1alpha1.CapabilityScopeWorld,
				Multiplicity: gamev1alpha1.MultiplicityOne,
			}}, nil),
			mm("physics-b", []gamev1alpha1.ProvidedCapability{{
				CapabilityID: "cap.physics",
				Version:      "1.5.0",
				Scope:        gamev1alpha1.CapabilityScopeWorld,
				Multiplicity: gamev1alpha1.MultiplicityOne,
			}}, nil),
			mm("interaction", nil, []gamev1alpha1.RequiredCapability{{
				CapabilityID:      "cap.physics",
				VersionConstraint: ">=1.0.0 <2.0.0",
				Scope:             gamev1alpha1.CapabilityScopeWorld,
				Multiplicity:      gamev1alpha1.MultiplicityOne,
				DependencyMode:    gamev1alpha1.DependencyModeRequired,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if len(plan.Diagnostics.UnresolvedRequired) != 0 {
		t.Fatalf("expected no unresolved required, got: %+v", plan.Diagnostics.UnresolvedRequired)
	}
	if len(plan.DesiredBindings) != 1 {
		t.Fatalf("expected 1 desired binding, got %d", len(plan.DesiredBindings))
	}

	b := plan.DesiredBindings[0]
	if b.Spec.Consumer.ModuleManifestName != "interaction" {
		t.Fatalf("expected consumer=interaction, got %q", b.Spec.Consumer.ModuleManifestName)
	}
	if b.Spec.Provider.ModuleManifestName != "physics-b" {
		t.Fatalf("expected provider=physics-b, got %q", b.Spec.Provider.ModuleManifestName)
	}
	if b.Spec.Provider.CapabilityVersion != "1.5.0" {
		t.Fatalf("expected provider version=1.5.0, got %q", b.Spec.Provider.CapabilityVersion)
	}
}

func TestDefaultResolver_TieBreaksByModuleName(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: gamev1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []gamev1alpha1.ModuleManifest{
			mm("provider-b", []gamev1alpha1.ProvidedCapability{{
				CapabilityID: "cap.time",
				Version:      "1.0.0",
				Scope:        gamev1alpha1.CapabilityScopeWorld,
				Multiplicity: gamev1alpha1.MultiplicityOne,
			}}, nil),
			mm("provider-a", []gamev1alpha1.ProvidedCapability{{
				CapabilityID: "cap.time",
				Version:      "1.0.0",
				Scope:        gamev1alpha1.CapabilityScopeWorld,
				Multiplicity: gamev1alpha1.MultiplicityOne,
			}}, nil),
			mm("consumer", nil, []gamev1alpha1.RequiredCapability{{
				CapabilityID:      "cap.time",
				VersionConstraint: "=1.0.0",
				Scope:             gamev1alpha1.CapabilityScopeWorld,
				Multiplicity:      gamev1alpha1.MultiplicityOne,
				DependencyMode:    gamev1alpha1.DependencyModeRequired,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if len(plan.DesiredBindings) != 1 {
		t.Fatalf("expected 1 desired binding, got %d", len(plan.DesiredBindings))
	}
	if plan.DesiredBindings[0].Spec.Provider.ModuleManifestName != "provider-a" {
		t.Fatalf("expected provider-a due to tie-break, got %q", plan.DesiredBindings[0].Spec.Provider.ModuleManifestName)
	}
}

func TestDefaultResolver_MultiplicityManySelectsAll(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: gamev1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []gamev1alpha1.ModuleManifest{
			mm("p1", []gamev1alpha1.ProvidedCapability{{
				CapabilityID: "cap.events",
				Version:      "1.0.0",
				Scope:        gamev1alpha1.CapabilityScopeWorld,
				Multiplicity: gamev1alpha1.MultiplicityMany,
			}}, nil),
			mm("p2", []gamev1alpha1.ProvidedCapability{{
				CapabilityID: "cap.events",
				Version:      "1.1.0",
				Scope:        gamev1alpha1.CapabilityScopeWorld,
				Multiplicity: gamev1alpha1.MultiplicityMany,
			}}, nil),
			mm("c", nil, []gamev1alpha1.RequiredCapability{{
				CapabilityID:      "cap.events",
				VersionConstraint: ">=1.0.0 <2.0.0",
				Scope:             gamev1alpha1.CapabilityScopeWorld,
				Multiplicity:      gamev1alpha1.MultiplicityMany,
				DependencyMode:    gamev1alpha1.DependencyModeRequired,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if len(plan.DesiredBindings) != 2 {
		t.Fatalf("expected 2 desired bindings, got %d", len(plan.DesiredBindings))
	}

	// Final Plan output is sorted by provider module name, so p1 then p2.
	if plan.DesiredBindings[0].Spec.Provider.ModuleManifestName != "p1" {
		t.Fatalf("expected first provider p1, got %q", plan.DesiredBindings[0].Spec.Provider.ModuleManifestName)
	}
	if plan.DesiredBindings[1].Spec.Provider.ModuleManifestName != "p2" {
		t.Fatalf("expected second provider p2, got %q", plan.DesiredBindings[1].Spec.Provider.ModuleManifestName)
	}
}

func TestDefaultResolver_UnresolvedOptionalRecorded(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: gamev1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []gamev1alpha1.ModuleManifest{
			mm("consumer", nil, []gamev1alpha1.RequiredCapability{{
				CapabilityID:      "cap.logging",
				VersionConstraint: ">=1.0.0",
				Scope:             gamev1alpha1.CapabilityScopeWorld,
				Multiplicity:      gamev1alpha1.MultiplicityOne,
				DependencyMode:    gamev1alpha1.DependencyModeOptional,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if len(plan.DesiredBindings) != 0 {
		t.Fatalf("expected 0 bindings, got %d", len(plan.DesiredBindings))
	}
	if len(plan.Diagnostics.UnresolvedOptional) != 1 {
		t.Fatalf("expected 1 unresolved optional, got %d", len(plan.Diagnostics.UnresolvedOptional))
	}
}

func TestDefaultResolver_UnresolvedRequiredRecorded(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: gamev1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []gamev1alpha1.ModuleManifest{
			mm("consumer", nil, []gamev1alpha1.RequiredCapability{{
				CapabilityID:      "cap.missing",
				VersionConstraint: ">=1.0.0",
				Scope:             gamev1alpha1.CapabilityScopeWorld,
				Multiplicity:      gamev1alpha1.MultiplicityOne,
				DependencyMode:    gamev1alpha1.DependencyModeRequired,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if len(plan.DesiredBindings) != 0 {
		t.Fatalf("expected 0 bindings, got %d", len(plan.DesiredBindings))
	}
	if len(plan.Diagnostics.UnresolvedRequired) != 1 {
		t.Fatalf("expected 1 unresolved required, got %d", len(plan.Diagnostics.UnresolvedRequired))
	}
	if plan.Diagnostics.UnresolvedRequired[0].CapabilityID != "cap.missing" {
		t.Fatalf("expected unresolved cap.missing, got %s", plan.Diagnostics.UnresolvedRequired[0].CapabilityID)
	}
}

func TestDefaultResolver_VersionIncompatibleRequired(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: gamev1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []gamev1alpha1.ModuleManifest{
			mm("provider", []gamev1alpha1.ProvidedCapability{{
				CapabilityID: "cap.physics",
				Version:      "1.0.0",
				Scope:        gamev1alpha1.CapabilityScopeWorld,
				Multiplicity: gamev1alpha1.MultiplicityOne,
			}}, nil),
			mm("consumer", nil, []gamev1alpha1.RequiredCapability{{
				CapabilityID:      "cap.physics",
				VersionConstraint: ">=2.0.0",
				Scope:             gamev1alpha1.CapabilityScopeWorld,
				Multiplicity:      gamev1alpha1.MultiplicityOne,
				DependencyMode:    gamev1alpha1.DependencyModeRequired,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if len(plan.DesiredBindings) != 0 {
		t.Fatalf("expected 0 bindings, got %d", len(plan.DesiredBindings))
	}
	if len(plan.Diagnostics.UnresolvedRequired) != 1 {
		t.Fatalf("expected 1 unresolved required, got %d", len(plan.Diagnostics.UnresolvedRequired))
	}
}

func TestDefaultResolver_VersionIncompatibleOptional(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: gamev1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []gamev1alpha1.ModuleManifest{
			mm("provider", []gamev1alpha1.ProvidedCapability{{
				CapabilityID: "cap.physics",
				Version:      "1.0.0",
				Scope:        gamev1alpha1.CapabilityScopeWorld,
				Multiplicity: gamev1alpha1.MultiplicityOne,
			}}, nil),
			mm("consumer", nil, []gamev1alpha1.RequiredCapability{{
				CapabilityID:      "cap.physics",
				VersionConstraint: ">=2.0.0",
				Scope:             gamev1alpha1.CapabilityScopeWorld,
				Multiplicity:      gamev1alpha1.MultiplicityOne,
				DependencyMode:    gamev1alpha1.DependencyModeOptional,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if len(plan.DesiredBindings) != 0 {
		t.Fatalf("expected 0 bindings, got %d", len(plan.DesiredBindings))
	}
	if len(plan.Diagnostics.UnresolvedOptional) != 1 {
		t.Fatalf("expected 1 unresolved optional, got %d", len(plan.Diagnostics.UnresolvedOptional))
	}
}
