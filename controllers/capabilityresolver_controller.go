package controllers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
	"github.com/anvil-platform/anvil/internal/resolver"
)

const (
	labelManagedBy = "game.platform/managed-by"
	labelWorldName = "game.platform/world"
	labelGameName  = "game.platform/game"

	managedByCapabilityResolver = "capabilityresolver"
)

var (
	reNonDNS = regexp.MustCompile(`[^a-z0-9-]+`)
)

// CapabilityResolverReconciler reconciles WorldInstances into CapabilityBindings.
//
// RBAC:
// +kubebuilder:rbac:groups=game.platform,resources=modulemanifests,verbs=get;list;watch
// +kubebuilder:rbac:groups=game.platform,resources=gamedefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=game.platform,resources=worldinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=game.platform,resources=capabilitybindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=game.platform,resources=capabilitybindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=game.platform,resources=worldinstances/status,verbs=get;update;patch
//
// NOTE: Business logic intentionally omitted. This is framework wiring only.
type CapabilityResolverReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Resolver resolver.Resolver
}

func (r *CapabilityResolverReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		"controller", "CapabilityResolver",
		"namespace", req.Namespace,
		"name", req.Name,
	)

	// 1) Load WorldInstance
	var world gamev1alpha1.WorldInstance
	if err := r.Get(ctx, req.NamespacedName, &world); err != nil {
		// Ignore not-found errors: object was deleted.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger = logger.WithValues(
		"worldId", world.Spec.WorldID,
		"game", world.Spec.GameRef.Name,
	)
	logger.Info("reconciling world")

	if r.Resolver == nil {
		// Should be injected in main(), but keep a safe default.
		r.Resolver = resolver.NewDefault()
	}

	// 2) Load referenced GameDefinition
	var game gamev1alpha1.GameDefinition
	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: world.Spec.GameRef.Name}, &game); err != nil {
		if apierrors.IsNotFound(err) {
			_ = r.patchWorldStatus(ctx, &world, "Error", "GameDefinitionNotFound")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 3) Load participating ModuleManifests from GameDefinition.spec.modules
	modules := make([]gamev1alpha1.ModuleManifest, 0, len(game.Spec.Modules))
	missingRequired := make([]string, 0)
	for _, ref := range game.Spec.Modules {
		var mm gamev1alpha1.ModuleManifest
		if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: ref.Name}, &mm); err != nil {
			if apierrors.IsNotFound(err) {
				if ref.Required {
					missingRequired = append(missingRequired, ref.Name)
				}
				continue
			}
			return ctrl.Result{}, err
		}
		modules = append(modules, mm)
	}

	if len(missingRequired) > 0 {
		_ = r.patchWorldStatus(ctx, &world, "Error", fmt.Sprintf("ModuleManifestNotFound: %s", strings.Join(missingRequired, ", ")))
		return ctrl.Result{}, nil
	}

	// 4) Resolve bindings
	plan, err := r.Resolver.Resolve(ctx, resolver.Input{World: world, Game: game, Modules: modules})
	if err != nil {
		// Resolver errors are treated as config errors (schema-valid but semantically invalid).
		_ = r.patchWorldStatus(ctx, &world, "Error", fmt.Sprintf("ResolveError: %v", err))
		return ctrl.Result{}, nil
	}

	// 5) Apply desired bindings
	desiredNames := make(map[string]struct{}, len(plan.DesiredBindings))
	for _, desired := range plan.DesiredBindings {
		bindingName := stableBindingName(world.Name,
			desired.Spec.Consumer.ModuleManifestName,
			desired.Spec.CapabilityID,
			desired.Spec.Scope,
			desired.Spec.Multiplicity,
		)
		desiredNames[bindingName] = struct{}{}

		obj := &gamev1alpha1.CapabilityBinding{}
		err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: bindingName}, obj)
		if apierrors.IsNotFound(err) {
			create := gamev1alpha1.CapabilityBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bindingName,
					Namespace: req.Namespace,
					Labels: map[string]string{
						labelManagedBy: managedByCapabilityResolver,
						labelWorldName: world.Name,
						labelGameName:  game.Name,
					},
				},
				Spec: desired.Spec,
			}
			// Ensure spec.worldRef is always set.
			create.Spec.WorldRef = &gamev1alpha1.WorldRef{Name: world.Name}
			if err := controllerutil.SetControllerReference(&world, &create, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, &create); err != nil {
				return ctrl.Result{}, err
			}
			continue
		}
		if err != nil {
			return ctrl.Result{}, err
		}

		before := obj.DeepCopy()
		if obj.Labels == nil {
			obj.Labels = map[string]string{}
		}
		obj.Labels[labelManagedBy] = managedByCapabilityResolver
		obj.Labels[labelWorldName] = world.Name
		obj.Labels[labelGameName] = game.Name
		obj.Spec.CapabilityID = desired.Spec.CapabilityID
		obj.Spec.Scope = desired.Spec.Scope
		obj.Spec.Multiplicity = desired.Spec.Multiplicity
		obj.Spec.WorldRef = &gamev1alpha1.WorldRef{Name: world.Name}
		obj.Spec.Consumer = desired.Spec.Consumer
		obj.Spec.Provider = desired.Spec.Provider
		if err := controllerutil.SetControllerReference(&world, obj, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.Patch(ctx, obj, client.MergeFrom(before)); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 6) Garbage-collect stale bindings that we manage for this world.
	var existing gamev1alpha1.CapabilityBindingList
	if err := r.List(ctx, &existing,
		client.InNamespace(req.Namespace),
		client.MatchingLabels{
			labelManagedBy: managedByCapabilityResolver,
			labelWorldName: world.Name,
		},
	); err != nil {
		return ctrl.Result{}, err
	}
	for i := range existing.Items {
		b := &existing.Items[i]
		if _, ok := desiredNames[b.Name]; ok {
			continue
		}
		if err := r.Delete(ctx, b); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	// 7) Surface diagnostics in WorldInstance.status
	if len(plan.Diagnostics.UnresolvedRequired) > 0 {
		_ = r.patchWorldStatus(ctx, &world, "Error", summarizeUnresolved(plan.Diagnostics.UnresolvedRequired))
		return ctrl.Result{}, nil
	}
	message := "All required bindings resolved"
	if len(plan.Diagnostics.UnresolvedOptional) > 0 {
		message = fmt.Sprintf("All required bindings resolved (%d optional unresolved)", len(plan.Diagnostics.UnresolvedOptional))
	}
	_ = r.patchWorldStatus(ctx, &world, "Running", message)

	return ctrl.Result{}, nil
}

func (r *CapabilityResolverReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Field indexers for efficient fan-out.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gamev1alpha1.WorldInstance{}, ".spec.gameRef.name", func(obj client.Object) []string {
		w, ok := obj.(*gamev1alpha1.WorldInstance)
		if !ok {
			return nil
		}
		if w.Spec.GameRef.Name == "" {
			return nil
		}
		return []string{w.Spec.GameRef.Name}
	}); err != nil {
		return err
	}
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gamev1alpha1.GameDefinition{}, ".spec.modules[*].name", func(obj client.Object) []string {
		g, ok := obj.(*gamev1alpha1.GameDefinition)
		if !ok {
			return nil
		}
		out := make([]string, 0, len(g.Spec.Modules))
		for _, m := range g.Spec.Modules {
			if m.Name != "" {
				out = append(out, m.Name)
			}
		}
		return out
	}); err != nil {
		return err
	}

	// Reconcile is driven primarily by WorldInstance.
	b := ctrl.NewControllerManagedBy(mgr).
		For(&gamev1alpha1.WorldInstance{}).
		Owns(&gamev1alpha1.CapabilityBinding{})

	// Watch GameDefinition changes and enqueue referencing worlds.
	b = b.Watches(
		&gamev1alpha1.GameDefinition{},
		enqueueWorldsForGame(mgr.GetClient()),
	)

	// Watch ModuleManifest changes and enqueue referencing worlds.
	b = b.Watches(
		&gamev1alpha1.ModuleManifest{},
		enqueueWorldsForModule(mgr.GetClient()),
	)

	return b.Complete(r)
}

// enqueueWorldsForGame returns an event handler that enqueues WorldInstances impacted by a GameDefinition.
//
// TODO(business-logic): Implement indexing/lookup strategy.
func enqueueWorldsForGame(c client.Client) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		game, ok := obj.(*gamev1alpha1.GameDefinition)
		if !ok {
			return nil
		}

		var worlds gamev1alpha1.WorldInstanceList
		if err := c.List(ctx, &worlds,
			client.InNamespace(game.Namespace),
			client.MatchingFields{".spec.gameRef.name": game.Name},
		); err != nil {
			return nil
		}

		out := make([]reconcile.Request, 0, len(worlds.Items))
		for i := range worlds.Items {
			w := &worlds.Items[i]
			out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: w.Namespace, Name: w.Name}})
		}
		return out
	})
}

// enqueueWorldsForModule returns an event handler that enqueues WorldInstances impacted by a ModuleManifest.
//
// TODO(business-logic): Implement indexing/lookup strategy.
func enqueueWorldsForModule(c client.Client) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		mm, ok := obj.(*gamev1alpha1.ModuleManifest)
		if !ok {
			return nil
		}

		var games gamev1alpha1.GameDefinitionList
		if err := c.List(ctx, &games,
			client.InNamespace(mm.Namespace),
			client.MatchingFields{".spec.modules[*].name": mm.Name},
		); err != nil {
			return nil
		}

		out := make([]reconcile.Request, 0)
		for i := range games.Items {
			g := &games.Items[i]
			var worlds gamev1alpha1.WorldInstanceList
			if err := c.List(ctx, &worlds,
				client.InNamespace(mm.Namespace),
				client.MatchingFields{".spec.gameRef.name": g.Name},
			); err != nil {
				continue
			}
			for j := range worlds.Items {
				w := &worlds.Items[j]
				out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: w.Namespace, Name: w.Name}})
			}
		}

		return out
	})
}

func (r *CapabilityResolverReconciler) patchWorldStatus(ctx context.Context, world *gamev1alpha1.WorldInstance, phase, message string) error {
	before := world.DeepCopy()
	world.Status.Phase = phase
	world.Status.Message = message
	return r.Status().Patch(ctx, world, client.MergeFrom(before))
}

func summarizeUnresolved(reqs []resolver.UnresolvedRequirement) string {
	// Keep this human-readable and bounded.
	if len(reqs) == 0 {
		return ""
	}
	max := 4
	parts := make([]string, 0, min(len(reqs), max))
	for i := 0; i < len(reqs) && i < max; i++ {
		r := reqs[i]
		parts = append(parts, fmt.Sprintf("%s requires %s (%s)", r.ConsumerModuleManifestName, r.CapabilityID, r.Reason))
	}
	if len(reqs) > max {
		parts = append(parts, fmt.Sprintf("...and %d more", len(reqs)-max))
	}
	return strings.Join(parts, "; ")
}

func stableBindingName(worldName, consumerModuleName, capabilityID string, scope gamev1alpha1.CapabilityScope, multiplicity gamev1alpha1.CapabilityMultiplicity) string {
	// K8s object names must be DNS subdomains (we keep it conservative: DNS labels).
	base := fmt.Sprintf("cb-%s-%s-%s-%s-%s",
		worldName,
		consumerModuleName,
		strings.ReplaceAll(capabilityID, ".", "-"),
		string(scope),
		string(multiplicity),
	)
	base = strings.ToLower(base)
	base = reNonDNS.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "cb"
	}
	// Ensure it starts/ends with alnum.
	base = strings.Trim(base, "-")
	base = strings.Trim(base, ".")
	// Bound length; add hash suffix if truncating.
	if len(base) <= 253 {
		return base
	}

	h := sha1.Sum([]byte(base))
	suffix := "-" + hex.EncodeToString(h[:])[:8]
	trimTo := 253 - len(suffix)
	if trimTo < 1 {
		return hex.EncodeToString(h[:])[:16]
	}
	base = strings.Trim(base[:trimTo], "-")
	return base + suffix
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
