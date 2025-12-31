package resolver

import (
	"context"
	"sort"
	"strings"

	binderyv1alpha1 "github.com/bayleafwalker/bindery-core/api/v1alpha1"
	"github.com/bayleafwalker/bindery-core/internal/semver"
)

// DefaultResolver is the default implementation wired into the controller.
//
// TODO(business-logic): implement semver/scope/multiplicity resolution.
type DefaultResolver struct{}

type provider struct {
	moduleName   string
	capabilityID string
	versionRaw   string
	version      semver.Version
	scope        binderyv1alpha1.CapabilityScope
	multiplicity binderyv1alpha1.CapabilityMultiplicity
}

func NewDefault() *DefaultResolver {
	return &DefaultResolver{}
}

func (r *DefaultResolver) Resolve(ctx context.Context, in Input) (Plan, error) {
	_ = ctx

	providers := make([]provider, 0)
	// Helper to add providers from a list of modules
	addProviders := func(modules []binderyv1alpha1.ModuleManifest) {
		for _, module := range modules {
			for _, provided := range module.Spec.Provides {
				v, err := semver.ParseVersion(strings.TrimSpace(provided.Version))
				if err != nil {
					continue
				}
				providers = append(providers, provider{
					moduleName:   module.Name,
					capabilityID: provided.CapabilityID,
					versionRaw:   strings.TrimSpace(provided.Version),
					version:      v,
					scope:        provided.Scope,
					multiplicity: provided.Multiplicity,
				})
			}
		}
	}

	addProviders(in.Modules)
	addProviders(in.ExternalModules)

	plan := Plan{}

	for _, consumer := range in.Modules {
		for _, req := range consumer.Spec.Requires {
			rawConstraint := strings.TrimSpace(req.VersionConstraint)
			if rawConstraint == "" {
				rawConstraint = "*"
			}

			constraint, err := semver.ParseConstraint(rawConstraint)
			if err != nil {
				addUnresolved(&plan.Diagnostics, consumer.Name, req, "invalid versionConstraint")
				continue
			}

			candidates := make([]provider, 0)
			for _, p := range providers {
				if p.capabilityID != req.CapabilityID {
					continue
				}
				if p.scope != req.Scope {
					continue
				}
				reqMultiplicity := req.Multiplicity
				if strings.TrimSpace(string(reqMultiplicity)) == "" {
					reqMultiplicity = binderyv1alpha1.MultiplicityOne
				}
				providerMultiplicity := p.multiplicity
				if strings.TrimSpace(string(providerMultiplicity)) == "" {
					providerMultiplicity = binderyv1alpha1.MultiplicityOne
				}
				// Multiplicity compatibility matrix:
				// - require "1"   -> provider "1" or "many"
				// - require "many"-> provider must be "many"
				if reqMultiplicity == binderyv1alpha1.MultiplicityMany && providerMultiplicity != binderyv1alpha1.MultiplicityMany {
					continue
				}
				if !semver.Satisfies(p.version, constraint) {
					continue
				}
				candidates = append(candidates, p)
			}

			if len(candidates) == 0 {
				addUnresolved(&plan.Diagnostics, consumer.Name, req, "no compatible provider found")
				continue
			}

			selected := selectProvidersDeterministic(req.Multiplicity, candidates)
			for _, p := range selected {
				plan.DesiredBindings = append(plan.DesiredBindings, binderyv1alpha1.CapabilityBinding{
					Spec: binderyv1alpha1.CapabilityBindingSpec{
						CapabilityID: req.CapabilityID,
						Scope:        req.Scope,
						Multiplicity: req.Multiplicity,
						WorldRef:     &binderyv1alpha1.WorldRef{Name: in.World.Name},
						Consumer: binderyv1alpha1.ConsumerRef{
							ModuleManifestName: consumer.Name,
							Requirement: &binderyv1alpha1.RequirementHint{
								VersionConstraint: rawConstraint,
								DependencyMode:    req.DependencyMode,
							},
						},
						Provider: binderyv1alpha1.ProviderRef{
							ModuleManifestName: p.moduleName,
							CapabilityVersion:  p.versionRaw,
						},
					},
				})
			}
		}
	}

	// Ensure all modules in the Booklet are running.
	// If a module is not a provider in any binding, create a synthetic "root" binding.
	for _, module := range in.Modules {
		if !isProvider(module.Name, plan.DesiredBindings) {
			scope := module.Spec.Scaling.DefaultScope
			if strings.TrimSpace(string(scope)) == "" {
				scope = binderyv1alpha1.CapabilityScopeWorld
			}
			plan.DesiredBindings = append(plan.DesiredBindings, binderyv1alpha1.CapabilityBinding{
				Spec: binderyv1alpha1.CapabilityBindingSpec{
					CapabilityID: "system.root",
					Scope:        scope,
					Multiplicity: binderyv1alpha1.MultiplicityOne,
					WorldRef:     &binderyv1alpha1.WorldRef{Name: in.World.Name},
					Consumer: binderyv1alpha1.ConsumerRef{
						ModuleManifestName: in.World.Name,
						Requirement: &binderyv1alpha1.RequirementHint{
							VersionConstraint: "*",
							DependencyMode:    binderyv1alpha1.DependencyModeRequired,
						},
					},
					Provider: binderyv1alpha1.ProviderRef{
						ModuleManifestName: module.Name,
						CapabilityVersion:  module.Spec.Module.Version,
					},
				},
			})
		}
	}

	sort.Slice(plan.DesiredBindings, func(i, j int) bool {
		a := plan.DesiredBindings[i].Spec
		b := plan.DesiredBindings[j].Spec
		if a.Consumer.ModuleManifestName != b.Consumer.ModuleManifestName {
			return a.Consumer.ModuleManifestName < b.Consumer.ModuleManifestName
		}
		if a.CapabilityID != b.CapabilityID {
			return a.CapabilityID < b.CapabilityID
		}
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		if a.Provider.ModuleManifestName != b.Provider.ModuleManifestName {
			return a.Provider.ModuleManifestName < b.Provider.ModuleManifestName
		}
		return a.Provider.CapabilityVersion < b.Provider.CapabilityVersion
	})

	return plan, nil
}

func addUnresolved(diag *Diagnostics, consumerModuleName string, req binderyv1alpha1.RequiredCapability, reason string) {
	unresolved := UnresolvedRequirement{
		ConsumerModuleManifestName: consumerModuleName,
		CapabilityID:               req.CapabilityID,
		Scope:                      req.Scope,
		Reason:                     reason,
	}
	if req.DependencyMode == binderyv1alpha1.DependencyModeOptional {
		diag.UnresolvedOptional = append(diag.UnresolvedOptional, unresolved)
		return
	}
	diag.UnresolvedRequired = append(diag.UnresolvedRequired, unresolved)
}

func selectProvidersDeterministic(multiplicity binderyv1alpha1.CapabilityMultiplicity, candidates []provider) []provider {
	// Deterministic ordering:
	// 1) Higher version wins
	// 2) Tie-break: module name (ascending)
	sort.Slice(candidates, func(i, j int) bool {
		vi := candidates[i].version
		vj := candidates[j].version
		cmp := semver.Compare(vi, vj)
		if cmp != 0 {
			return cmp > 0
		}
		return candidates[i].moduleName < candidates[j].moduleName
	})

	if multiplicity == binderyv1alpha1.MultiplicityMany {
		return candidates
	}
	return candidates[:1]
}

func isProvider(moduleName string, bindings []binderyv1alpha1.CapabilityBinding) bool {
	for _, b := range bindings {
		if b.Spec.Provider.ModuleManifestName == moduleName {
			return true
		}
	}
	return false
}
