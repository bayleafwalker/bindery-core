package resolver

import (
	"context"
	"sort"
	"strings"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
	"github.com/anvil-platform/anvil/internal/semver"
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
	scope        gamev1alpha1.CapabilityScope
	multiplicity gamev1alpha1.CapabilityMultiplicity
}

func NewDefault() *DefaultResolver {
	return &DefaultResolver{}
}

func (r *DefaultResolver) Resolve(ctx context.Context, in Input) (Plan, error) {
	_ = ctx

	providers := make([]provider, 0)
	for _, module := range in.Modules {
		for _, provided := range module.Spec.Provides {
			v, err := semver.ParseVersion(strings.TrimSpace(provided.Version))
			if err != nil {
				// Ignore invalid provider versions; they simply won't be considered.
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
				if req.Multiplicity != "" && p.multiplicity != "" && p.multiplicity != req.Multiplicity {
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
				plan.DesiredBindings = append(plan.DesiredBindings, gamev1alpha1.CapabilityBinding{
					Spec: gamev1alpha1.CapabilityBindingSpec{
						CapabilityID: req.CapabilityID,
						Scope:        req.Scope,
						Multiplicity: req.Multiplicity,
						WorldRef:     &gamev1alpha1.WorldRef{Name: in.World.Name},
						Consumer: gamev1alpha1.ConsumerRef{
							ModuleManifestName: consumer.Name,
							Requirement: &gamev1alpha1.RequirementHint{
								VersionConstraint: rawConstraint,
								DependencyMode:    req.DependencyMode,
							},
						},
						Provider: gamev1alpha1.ProviderRef{
							ModuleManifestName: p.moduleName,
							CapabilityVersion:  p.versionRaw,
						},
					},
				})
			}
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

func addUnresolved(diag *Diagnostics, consumerModuleName string, req gamev1alpha1.RequiredCapability, reason string) {
	unresolved := UnresolvedRequirement{
		ConsumerModuleManifestName: consumerModuleName,
		CapabilityID:               req.CapabilityID,
		Scope:                      req.Scope,
		Reason:                     reason,
	}
	if req.DependencyMode == gamev1alpha1.DependencyModeOptional {
		diag.UnresolvedOptional = append(diag.UnresolvedOptional, unresolved)
		return
	}
	diag.UnresolvedRequired = append(diag.UnresolvedRequired, unresolved)
}

func selectProvidersDeterministic(multiplicity gamev1alpha1.CapabilityMultiplicity, candidates []provider) []provider {
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

	if multiplicity == gamev1alpha1.MultiplicityMany {
		return candidates
	}
	return candidates[:1]
}
