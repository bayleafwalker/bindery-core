package resolver

import (
	"context"
	"testing"

	binderyv1alpha1 "github.com/bayleafwalker/bindery-core/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDefaultResolver_CreatesRootBindings(t *testing.T) {
	r := NewDefault()

	in := Input{
		World: binderyv1alpha1.WorldInstance{ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"}},
		Modules: []binderyv1alpha1.ModuleManifest{
			// Module A: Root module, no requirements, no providers.
			mm("module-a", nil, nil),
			// Module B: Required by Module C.
			mm("module-b", []binderyv1alpha1.ProvidedCapability{{
				CapabilityID: "cap.b",
				Version:      "1.0.0",
				Scope:        binderyv1alpha1.CapabilityScopeWorld,
				Multiplicity: binderyv1alpha1.MultiplicityOne,
			}}, nil),
			// Module C: Requires Module B.
			mm("module-c", nil, []binderyv1alpha1.RequiredCapability{{
				CapabilityID:      "cap.b",
				VersionConstraint: "*",
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

	// Expected bindings:
	// 1. module-c -> module-b (Explicit)
	// 2. world-1 -> module-a (Root)
	// 3. world-1 -> module-c (Root) - Wait, module-c is a consumer, but is it a provider? No. So it should also get a root binding.
	// module-b is a provider, so it won't get a root binding.

	if len(plan.DesiredBindings) != 3 {
		t.Fatalf("expected 3 desired bindings, got %d", len(plan.DesiredBindings))
	}

	// Verify module-a root binding
	foundA := false
	for _, b := range plan.DesiredBindings {
		if b.Spec.Provider.ModuleManifestName == "module-a" {
			foundA = true
			if b.Spec.CapabilityID != "system.root" {
				t.Errorf("expected module-a binding to have capabilityID 'system.root', got %q", b.Spec.CapabilityID)
			}
			if b.Spec.Consumer.ModuleManifestName != "world-1" {
				t.Errorf("expected module-a consumer to be 'world-1', got %q", b.Spec.Consumer.ModuleManifestName)
			}
		}
	}
	if !foundA {
		t.Error("did not find root binding for module-a")
	}

	// Verify module-c root binding
	foundC := false
	for _, b := range plan.DesiredBindings {
		if b.Spec.Provider.ModuleManifestName == "module-c" {
			foundC = true
			if b.Spec.CapabilityID != "system.root" {
				t.Errorf("expected module-c binding to have capabilityID 'system.root', got %q", b.Spec.CapabilityID)
			}
		}
	}
	if !foundC {
		t.Error("did not find root binding for module-c")
	}

	// Verify module-b is NOT a root binding (it's provided via cap.b)
	for _, b := range plan.DesiredBindings {
		if b.Spec.Provider.ModuleManifestName == "module-b" {
			if b.Spec.CapabilityID == "system.root" {
				t.Error("module-b should not have a root binding because it is provided by module-c's requirement")
			}
			if b.Spec.CapabilityID != "cap.b" {
				t.Errorf("expected module-b binding to have capabilityID 'cap.b', got %q", b.Spec.CapabilityID)
			}
		}
	}
}
