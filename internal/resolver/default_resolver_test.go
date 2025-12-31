package resolver

import (
	"context"
	"testing"

	binderyv1alpha1 "github.com/bayleafwalker/bindery-core/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mm(name string, provides []binderyv1alpha1.ProvidedCapability, requires []binderyv1alpha1.RequiredCapability) binderyv1alpha1.ModuleManifest {
	return binderyv1alpha1.ModuleManifest{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: binderyv1alpha1.ModuleManifestSpec{
			Module:   binderyv1alpha1.ModuleIdentity{ID: name, Version: "0.0.0"},
			Provides: provides,
			Requires: requires,
		},
	}
}

func TestDefaultResolver_SelectsHighestCompatibleProvider(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: binderyv1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []binderyv1alpha1.ModuleManifest{
			mm("physics-a", []binderyv1alpha1.ProvidedCapability{{
				CapabilityID: "cap.physics",
				Version:      "1.0.0",
				Scope:        binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity: binderyv1alpha1.MultiplicityOne,
			}}, nil),
			mm("physics-b", []binderyv1alpha1.ProvidedCapability{{
				CapabilityID: "cap.physics",
				Version:      "1.5.0",
				Scope:        binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity: binderyv1alpha1.MultiplicityOne,
			}}, nil),
			mm("interaction", nil, []binderyv1alpha1.RequiredCapability{{
				CapabilityID:      "cap.physics",
				VersionConstraint: ">=1.0.0 <2.0.0",
				Scope:             binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity:      binderyv1alpha1.MultiplicityOne,
				DependencyMode:    binderyv1alpha1.DependencyModeRequired,
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

	// Find the explicit binding for interaction -> physics
	var b *binderyv1alpha1.CapabilityBindingSpec
	for i := range plan.DesiredBindings {
		if plan.DesiredBindings[i].Spec.Consumer.ModuleManifestName == "interaction" {
			b = &plan.DesiredBindings[i].Spec
			break
		}
	}

	if b == nil {
		t.Fatal("expected binding for consumer 'interaction' not found")
	}

	if b.Provider.ModuleManifestName != "physics-b" {
		t.Fatalf("expected provider=physics-b, got %q", b.Provider.ModuleManifestName)
	}
	if b.Provider.CapabilityVersion != "1.5.0" {
		t.Fatalf("expected provider version=1.5.0, got %q", b.Provider.CapabilityVersion)
	}
}

func TestDefaultResolver_TieBreaksByModuleName(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: binderyv1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []binderyv1alpha1.ModuleManifest{
			mm("provider-b", []binderyv1alpha1.ProvidedCapability{{
				CapabilityID: "cap.time",
				Version:      "1.0.0",
				Scope:        binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity: binderyv1alpha1.MultiplicityOne,
			}}, nil),
			mm("provider-a", []binderyv1alpha1.ProvidedCapability{{
				CapabilityID: "cap.time",
				Version:      "1.0.0",
				Scope:        binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity: binderyv1alpha1.MultiplicityOne,
			}}, nil),
			mm("consumer", nil, []binderyv1alpha1.RequiredCapability{{
				CapabilityID:      "cap.time",
				VersionConstraint: "=1.0.0",
				Scope:             binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity:      binderyv1alpha1.MultiplicityOne,
				DependencyMode:    binderyv1alpha1.DependencyModeRequired,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	// Find the explicit binding
	var b *binderyv1alpha1.CapabilityBindingSpec
	for i := range plan.DesiredBindings {
		if plan.DesiredBindings[i].Spec.Consumer.ModuleManifestName == "consumer" {
			b = &plan.DesiredBindings[i].Spec
			break
		}
	}

	if b == nil {
		t.Fatal("expected binding for consumer 'consumer' not found")
	}

	if b.Provider.ModuleManifestName != "provider-a" {
		t.Fatalf("expected provider-a due to tie-break, got %q", b.Provider.ModuleManifestName)
	}
}

func TestDefaultResolver_MultiplicityManySelectsAll(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: binderyv1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []binderyv1alpha1.ModuleManifest{
			mm("p1", []binderyv1alpha1.ProvidedCapability{{
				CapabilityID: "cap.events",
				Version:      "1.0.0",
				Scope:        binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity: binderyv1alpha1.MultiplicityMany,
			}}, nil),
			mm("p2", []binderyv1alpha1.ProvidedCapability{{
				CapabilityID: "cap.events",
				Version:      "1.1.0",
				Scope:        binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity: binderyv1alpha1.MultiplicityMany,
			}}, nil),
			mm("c", nil, []binderyv1alpha1.RequiredCapability{{
				CapabilityID:      "cap.events",
				VersionConstraint: ">=1.0.0 <2.0.0",
				Scope:             binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity:      binderyv1alpha1.MultiplicityMany,
				DependencyMode:    binderyv1alpha1.DependencyModeRequired,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	// Find explicit bindings for consumer 'c'
	var bindings []binderyv1alpha1.CapabilityBindingSpec
	for _, b := range plan.DesiredBindings {
		if b.Spec.Consumer.ModuleManifestName == "c" {
			bindings = append(bindings, b.Spec)
		}
	}

	if len(bindings) != 2 {
		t.Fatalf("expected 2 explicit bindings for consumer 'c', got %d", len(bindings))
	}

	// Final Plan output is sorted by provider module name, so p1 then p2.
	if bindings[0].Provider.ModuleManifestName != "p1" {
		t.Fatalf("expected first provider p1, got %q", bindings[0].Provider.ModuleManifestName)
	}
	if bindings[1].Provider.ModuleManifestName != "p2" {
		t.Fatalf("expected second provider p2, got %q", bindings[1].Provider.ModuleManifestName)
	}
}

func TestDefaultResolver_UnresolvedOptionalRecorded(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: binderyv1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []binderyv1alpha1.ModuleManifest{
			mm("consumer", nil, []binderyv1alpha1.RequiredCapability{{
				CapabilityID:      "cap.logging",
				VersionConstraint: ">=1.0.0",
				Scope:             binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity:      binderyv1alpha1.MultiplicityOne,
				DependencyMode:    binderyv1alpha1.DependencyModeOptional,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	// We expect 1 binding (root binding for consumer), but 0 explicit bindings.
	// The test cares about UnresolvedOptional.
	if len(plan.Diagnostics.UnresolvedOptional) != 1 {
		t.Fatalf("expected 1 unresolved optional, got %d", len(plan.Diagnostics.UnresolvedOptional))
	}
}

func TestDefaultResolver_UnresolvedRequiredRecorded(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: binderyv1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []binderyv1alpha1.ModuleManifest{
			mm("consumer", nil, []binderyv1alpha1.RequiredCapability{{
				CapabilityID:      "cap.missing",
				VersionConstraint: ">=1.0.0",
				Scope:             binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity:      binderyv1alpha1.MultiplicityOne,
				DependencyMode:    binderyv1alpha1.DependencyModeRequired,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	// We expect 1 binding (root binding for consumer), but 0 explicit bindings.
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
		World: binderyv1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []binderyv1alpha1.ModuleManifest{
			mm("provider", []binderyv1alpha1.ProvidedCapability{{
				CapabilityID: "cap.physics",
				Version:      "1.0.0",
				Scope:        binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity: binderyv1alpha1.MultiplicityOne,
			}}, nil),
			mm("consumer", nil, []binderyv1alpha1.RequiredCapability{{
				CapabilityID:      "cap.physics",
				VersionConstraint: ">=2.0.0",
				Scope:             binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity:      binderyv1alpha1.MultiplicityOne,
				DependencyMode:    binderyv1alpha1.DependencyModeRequired,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	// Expect root bindings for provider and consumer (2 total), but 0 explicit.
	if len(plan.Diagnostics.UnresolvedRequired) != 1 {
		t.Fatalf("expected 1 unresolved required, got %d", len(plan.Diagnostics.UnresolvedRequired))
	}
}

func TestDefaultResolver_VersionIncompatibleOptional(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: binderyv1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []binderyv1alpha1.ModuleManifest{
			mm("provider", []binderyv1alpha1.ProvidedCapability{{
				CapabilityID: "cap.physics",
				Version:      "1.0.0",
				Scope:        binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity: binderyv1alpha1.MultiplicityOne,
			}}, nil),
			mm("consumer", nil, []binderyv1alpha1.RequiredCapability{{
				CapabilityID:      "cap.physics",
				VersionConstraint: ">=2.0.0",
				Scope:             binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity:      binderyv1alpha1.MultiplicityOne,
				DependencyMode:    binderyv1alpha1.DependencyModeOptional,
			}}),
		},
	}

	plan, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	// Expect root bindings for provider and consumer (2 total), but 0 explicit.
	if len(plan.Diagnostics.UnresolvedOptional) != 1 {
		t.Fatalf("expected 1 unresolved optional, got %d", len(plan.Diagnostics.UnresolvedOptional))
	}
}

func TestDefaultResolver_IgnoresInvalidProviderVersion(t *testing.T) {
r := NewDefault()

in := Input{
World: binderyv1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
Modules: []binderyv1alpha1.ModuleManifest{
mm("physics-bad", []binderyv1alpha1.ProvidedCapability{{
CapabilityID: "cap.physics",
Version:      "invalid-version",
Scope:        binderyv1alpha1.CapabilityScopeWorld,
Multiplicity: binderyv1alpha1.MultiplicityOne,
}}, nil),
mm("interaction", nil, []binderyv1alpha1.RequiredCapability{{
CapabilityID:      "cap.physics",
VersionConstraint: "*",
Scope:             binderyv1alpha1.CapabilityScopeWorld,
Multiplicity:      binderyv1alpha1.MultiplicityOne,
DependencyMode:    binderyv1alpha1.DependencyModeRequired,
}}),
},
}

plan, err := r.Resolve(context.Background(), in)
if err != nil {
t.Fatalf("Resolve failed: %v", err)
}

	nonRoot := 0
	for _, b := range plan.DesiredBindings {
		if b.Spec.CapabilityID != "system.root" {
			nonRoot++
		}
	}
	if nonRoot != 0 {
		t.Errorf("expected 0 non-root bindings, got %d", nonRoot)
	}
	if len(plan.Diagnostics.UnresolvedRequired) != 1 {
		t.Errorf("expected 1 unresolved required, got %d", len(plan.Diagnostics.UnresolvedRequired))
	}
}

func TestDefaultResolver_ReportsInvalidConstraint(t *testing.T) {
r := NewDefault()

in := Input{
World: binderyv1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
Modules: []binderyv1alpha1.ModuleManifest{
mm("physics-a", []binderyv1alpha1.ProvidedCapability{{
CapabilityID: "cap.physics",
Version:      "1.0.0",
Scope:        binderyv1alpha1.CapabilityScopeWorld,
Multiplicity: binderyv1alpha1.MultiplicityOne,
}}, nil),
mm("interaction", nil, []binderyv1alpha1.RequiredCapability{{
CapabilityID:      "cap.physics",
VersionConstraint: "invalid-constraint",
Scope:             binderyv1alpha1.CapabilityScopeWorld,
Multiplicity:      binderyv1alpha1.MultiplicityOne,
DependencyMode:    binderyv1alpha1.DependencyModeRequired,
}}),
},
}

plan, err := r.Resolve(context.Background(), in)
if err != nil {
t.Fatalf("Resolve failed: %v", err)
}

	nonRoot := 0
	for _, b := range plan.DesiredBindings {
		if b.Spec.CapabilityID != "system.root" {
			nonRoot++
		}
	}
	if nonRoot != 0 {
		t.Errorf("expected 0 non-root bindings, got %d", nonRoot)
	}
	if len(plan.Diagnostics.UnresolvedRequired) != 1 {
		t.Errorf("expected 1 unresolved required, got %d", len(plan.Diagnostics.UnresolvedRequired))
	}
	if plan.Diagnostics.UnresolvedRequired[0].Reason != "invalid versionConstraint" {
		t.Errorf("expected reason 'invalid versionConstraint', got %q", plan.Diagnostics.UnresolvedRequired[0].Reason)
	}
}

func TestDefaultResolver_FiltersByMultiplicity(t *testing.T) {
r := NewDefault()

in := Input{
World: binderyv1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
Modules: []binderyv1alpha1.ModuleManifest{
mm("physics-one", []binderyv1alpha1.ProvidedCapability{{
CapabilityID: "cap.physics",
Version:      "1.0.0",
Scope:        binderyv1alpha1.CapabilityScopeWorld,
Multiplicity: binderyv1alpha1.MultiplicityOne,
}}, nil),
mm("interaction", nil, []binderyv1alpha1.RequiredCapability{{
CapabilityID:      "cap.physics",
VersionConstraint: "*",
Scope:             binderyv1alpha1.CapabilityScopeWorld,
Multiplicity:      binderyv1alpha1.MultiplicityMany, // Mismatch
DependencyMode:    binderyv1alpha1.DependencyModeRequired,
}}),
},
}

plan, err := r.Resolve(context.Background(), in)
if err != nil {
t.Fatalf("Resolve failed: %v", err)
}

	nonRoot := 0
	for _, b := range plan.DesiredBindings {
		if b.Spec.CapabilityID != "system.root" {
			nonRoot++
		}
	}
	if nonRoot != 0 {
		t.Errorf("expected 0 non-root bindings, got %d", nonRoot)
	}
	if len(plan.Diagnostics.UnresolvedRequired) != 1 {
		t.Errorf("expected 1 unresolved required, got %d", len(plan.Diagnostics.UnresolvedRequired))
	}
}
