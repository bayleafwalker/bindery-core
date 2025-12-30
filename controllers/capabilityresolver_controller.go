package controllers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
//
// NOTE: Business logic intentionally omitted. This is framework wiring only.
type CapabilityResolverReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Resolver resolver.Resolver
	Recorder record.EventRecorder
}

func (r *CapabilityResolverReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	anvilControllerReconcileTotal.WithLabelValues("CapabilityResolver").Inc()
	capabilityResolverUnresolvedRequired.Set(0)

	logger := log.FromContext(ctx).WithValues(
		"controller", "CapabilityResolver",
		"namespace", req.Namespace,
		"world", req.Name,
	)

	// 1) Load WorldInstance
	var world gamev1alpha1.WorldInstance
	if err := r.Get(ctx, req.NamespacedName, &world); err != nil {
		// Ignore not-found errors: object was deleted.
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		anvilControllerReconcileErrorTotal.WithLabelValues("CapabilityResolver").Inc()
		return ctrl.Result{}, err
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
			if perr := r.patchWorldStatus(ctx, &world, "Error", "GameDefinitionNotFound",
				metav1.Condition{
					Type:    WorldConditionModulesResolved,
					Status:  metav1.ConditionFalse,
					Reason:  "GameDefinitionNotFound",
					Message: fmt.Sprintf("GameDefinition %q not found", world.Spec.GameRef.Name),
				},
				metav1.Condition{
					Type:    WorldConditionBindingsResolved,
					Status:  metav1.ConditionFalse,
					Reason:  "ModulesNotReady",
					Message: "Cannot resolve bindings until required modules are loaded",
				},
			); perr != nil {
				logger.Error(perr, "failed to patch world status")
			}
			logger.Info("game definition not found; marking world error")
			r.recordEventf(&world, "Warning", "GameDefinitionNotFound", "GameDefinition %q not found", world.Spec.GameRef.Name)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to load game definition")
		r.recordEventf(&world, "Warning", "GetGameDefinitionFailed", "Failed to get GameDefinition %q: %v", world.Spec.GameRef.Name, err)
		anvilControllerReconcileErrorTotal.WithLabelValues("CapabilityResolver").Inc()
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
			logger.Error(err, "failed to load modulemanifest", "moduleManifest", ref.Name)
			return ctrl.Result{}, err
		}
		modules = append(modules, mm)
	}

	if len(missingRequired) > 0 {
		msg := fmt.Sprintf("ModuleManifestNotFound: %s", strings.Join(missingRequired, ", "))
		if perr := r.patchWorldStatus(ctx, &world, "Error", msg,
			metav1.Condition{
				Type:    WorldConditionModulesResolved,
				Status:  metav1.ConditionFalse,
				Reason:  "ModuleManifestNotFound",
				Message: fmt.Sprintf("Missing required ModuleManifest(s): %s", strings.Join(missingRequired, ", ")),
			},
			metav1.Condition{
				Type:    WorldConditionBindingsResolved,
				Status:  metav1.ConditionFalse,
				Reason:  "ModulesNotReady",
				Message: "Cannot resolve bindings until required modules are loaded",
			},
		); perr != nil {
			logger.Error(perr, "failed to patch world status")
		}
		logger.Info("required modulemanifests missing; marking world error", "missingModules", missingRequired)
		r.recordEventf(&world, "Warning", "ModuleManifestNotFound", "Required ModuleManifest(s) missing: %s", strings.Join(missingRequired, ", "))
		return ctrl.Result{}, nil
	}

	// 4) Resolve bindings
	plan, err := r.Resolver.Resolve(ctx, resolver.Input{World: world, Game: game, Modules: modules})
	if err != nil {
		// Resolver errors are treated as config errors (schema-valid but semantically invalid).
		msg := fmt.Sprintf("ResolveError: %v", err)
		if perr := r.patchWorldStatus(ctx, &world, "Error", msg,
			metav1.Condition{
				Type:    WorldConditionModulesResolved,
				Status:  metav1.ConditionTrue,
				Reason:  "ModulesLoaded",
				Message: "All required modules loaded",
			},
			metav1.Condition{
				Type:    WorldConditionBindingsResolved,
				Status:  metav1.ConditionFalse,
				Reason:  "ResolveError",
				Message: msg,
			},
		); perr != nil {
			logger.Error(perr, "failed to patch world status")
		}
		logger.Info("resolver returned error; marking world error")
		r.recordEventf(&world, "Warning", "ResolveError", "%s", msg)
		return ctrl.Result{}, nil
	}
	capabilityResolverUnresolvedRequired.Set(float64(len(plan.Diagnostics.UnresolvedRequired)))

	logger.Info(
		"resolved desired bindings",
		"moduleCount", len(modules),
		"desiredBindingCount", len(plan.DesiredBindings),
		"unresolvedRequiredCount", len(plan.Diagnostics.UnresolvedRequired),
		"unresolvedOptionalCount", len(plan.Diagnostics.UnresolvedOptional),
	)

	// 5) Apply desired bindings
	createdCount := 0
	updatedCount := 0
	desiredNames := make(map[string]struct{}, len(plan.DesiredBindings))

	// Pre-load shards for world-shard scoped bindings.
	var shards []gamev1alpha1.WorldShard
	{
		var shardList gamev1alpha1.WorldShardList
		if err := r.List(ctx, &shardList,
			client.InNamespace(req.Namespace),
			client.MatchingLabels{labelWorldName: world.Name},
		); err != nil {
			logger.Error(err, "failed to list worldshards")
			return ctrl.Result{}, err
		}
		shards = shardList.Items
	}

	for _, desired := range plan.DesiredBindings {
		logger.V(1).Info(
			"desired binding",
			"consumerModule", desired.Spec.Consumer.ModuleManifestName,
			"providerModule", desired.Spec.Provider.ModuleManifestName,
			"capabilityId", desired.Spec.CapabilityID,
			"scope", desired.Spec.Scope,
			"multiplicity", desired.Spec.Multiplicity,
			"chosenVersion", desired.Spec.Provider.CapabilityVersion,
		)

		// Expand world-shard bindings per shard.
		if desired.Spec.Scope == gamev1alpha1.CapabilityScopeWorldShard {
			if len(shards) == 0 {
				logger.Info("no shards found yet for world-shard bindings; waiting", "desiredShardCount", world.Spec.ShardCount)
				r.recordEventf(&world, "Normal", "WaitingForShards", "Waiting for WorldShards to exist")
				_ = r.patchWorldStatus(ctx, &world, "Provisioning", "Waiting for WorldShards",
					metav1.Condition{
						Type:    WorldConditionModulesResolved,
						Status:  metav1.ConditionTrue,
						Reason:  "ModulesLoaded",
						Message: "All required modules loaded",
					},
					metav1.Condition{
						Type:    WorldConditionBindingsResolved,
						Status:  metav1.ConditionFalse,
						Reason:  "WaitingForShards",
						Message: "WorldShard objects not found yet",
					},
				)
				return ctrl.Result{Requeue: true}, nil
			}
			for _, shard := range shards {
				bindingName := stableShardBindingName(world.Name, shard.Spec.ShardID,
					desired.Spec.Consumer.ModuleManifestName,
					desired.Spec.CapabilityID,
					desired.Spec.Scope,
					desired.Spec.Multiplicity,
				)
				desiredNames[bindingName] = struct{}{}
				c, u, err := r.applyDesiredBinding(ctx, req.Namespace, game.Name, world, &shard, bindingName, desired.Spec)
				if err != nil {
					logger.Error(err, "failed to apply desired shard binding", "binding", bindingName, "shard", shard.Spec.ShardID)
					return ctrl.Result{}, err
				}
				if c {
					createdCount++
				}
				if u {
					updatedCount++
				}
			}
			continue
		}

		bindingName := stableBindingName(world.Name,
			desired.Spec.Consumer.ModuleManifestName,
			desired.Spec.CapabilityID,
			desired.Spec.Scope,
			desired.Spec.Multiplicity,
		)
		desiredNames[bindingName] = struct{}{}
		c, u, err := r.applyDesiredBinding(ctx, req.Namespace, game.Name, world, nil, bindingName, desired.Spec)
		if err != nil {
			logger.Error(err, "failed to apply desired binding", "binding", bindingName)
			return ctrl.Result{}, err
		}
		if c {
			createdCount++
		}
		if u {
			updatedCount++
		}
	}
	logger.Info("applied desired bindings", "created", createdCount, "updated", updatedCount)
	if createdCount > 0 {
		capabilityResolverBindingsCreatedTotal.Add(float64(createdCount))
	}
	if updatedCount > 0 {
		capabilityResolverBindingsUpdatedTotal.Add(float64(updatedCount))
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
		logger.Error(err, "failed to list existing capabilitybindings")
		return ctrl.Result{}, err
	}
	deletedCount := 0
	for i := range existing.Items {
		b := &existing.Items[i]
		if _, ok := desiredNames[b.Name]; ok {
			continue
		}
		if err := r.Delete(ctx, b); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "failed to delete stale capabilitybinding", "binding", b.Name)
			return ctrl.Result{}, err
		}
		deletedCount++
	}
	if deletedCount > 0 {
		logger.Info("garbage-collected stale bindings", "deleted", deletedCount)
		capabilityResolverBindingsDeletedTotal.Add(float64(deletedCount))
	}
	if createdCount+updatedCount+deletedCount > 0 {
		r.recordEventf(&world, "Normal", "BindingsApplied", "Bindings applied (created=%d updated=%d deleted=%d)", createdCount, updatedCount, deletedCount)
	}

	// 7) Surface diagnostics in WorldInstance.status
	prevPhase := world.Status.Phase
	if len(plan.Diagnostics.UnresolvedRequired) > 0 {
		msg := summarizeUnresolved(plan.Diagnostics.UnresolvedRequired)
		if perr := r.patchWorldStatus(ctx, &world, "Error", msg,
			metav1.Condition{
				Type:    WorldConditionModulesResolved,
				Status:  metav1.ConditionTrue,
				Reason:  "ModulesLoaded",
				Message: "All required modules loaded",
			},
			metav1.Condition{
				Type:    WorldConditionBindingsResolved,
				Status:  metav1.ConditionFalse,
				Reason:  "UnresolvedRequired",
				Message: msg,
			},
		); perr != nil {
			logger.Error(perr, "failed to patch world status")
		}
		logger.Info("unresolved required bindings; marking world error")
		r.recordEventf(&world, "Warning", "UnresolvedRequiredBindings", "%s", msg)
		return ctrl.Result{}, nil
	}
	message := "All required bindings resolved"
	if len(plan.Diagnostics.UnresolvedOptional) > 0 {
		message = fmt.Sprintf("All required bindings resolved (%d optional unresolved)", len(plan.Diagnostics.UnresolvedOptional))
	}
	if perr := r.patchWorldStatus(ctx, &world, "Running", message,
		metav1.Condition{
			Type:    WorldConditionModulesResolved,
			Status:  metav1.ConditionTrue,
			Reason:  "ModulesLoaded",
			Message: "All required modules loaded",
		},
		metav1.Condition{
			Type:    WorldConditionBindingsResolved,
			Status:  metav1.ConditionTrue,
			Reason:  "Resolved",
			Message: message,
		},
	); perr != nil {
		logger.Error(perr, "failed to patch world status")
	}
	logger.Info("world resolved", "phase", "Running")
	if prevPhase != "Running" {
		// Avoid spamming; emit only on transitions.
		r.recordEventf(&world, "Normal", "BindingsResolved", "%s", message)
	}

	return ctrl.Result{}, nil
}

func (r *CapabilityResolverReconciler) recordEventf(obj client.Object, eventType, reason, messageFmt string, args ...any) {
	if r.Recorder == nil || obj == nil {
		return
	}
	r.Recorder.Eventf(obj, eventType, reason, messageFmt, args...)
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

	// Shards changing should trigger their owning world to reconcile (to refresh shard-scoped bindings).
	b = b.Watches(
		&gamev1alpha1.WorldShard{},
		handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			ws, ok := obj.(*gamev1alpha1.WorldShard)
			if !ok {
				return nil
			}
			if ws.Spec.WorldRef.Name == "" {
				return nil
			}
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: ws.Namespace, Name: ws.Spec.WorldRef.Name}}}
		}),
	)

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

func (r *CapabilityResolverReconciler) patchWorldStatus(ctx context.Context, world *gamev1alpha1.WorldInstance, phase, message string, conds ...metav1.Condition) error {
	before := world.DeepCopy()
	world.Status.ObservedGeneration = world.Generation
	world.Status.Phase = phase
	world.Status.Message = message
	for _, c := range conds {
		setWorldCondition(world, c)
	}
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

func stableShardBindingName(worldName string, shardID int32, consumerModuleName, capabilityID string, scope gamev1alpha1.CapabilityScope, multiplicity gamev1alpha1.CapabilityMultiplicity) string {
	base := fmt.Sprintf(
		"cb-%s-%s-%s-%s-%s-shard-%d",
		worldName,
		consumerModuleName,
		capabilityID,
		string(scope),
		string(multiplicity),
		shardID,
	)
	base = strings.ToLower(base)
	base = reNonDNS.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
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

func (r *CapabilityResolverReconciler) applyDesiredBinding(
	ctx context.Context,
	namespace string,
	gameName string,
	world gamev1alpha1.WorldInstance,
	shard *gamev1alpha1.WorldShard,
	bindingName string,
	spec gamev1alpha1.CapabilityBindingSpec,
) (created bool, updated bool, err error) {
	obj := &gamev1alpha1.CapabilityBinding{}
	err = r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: bindingName}, obj)
	if apierrors.IsNotFound(err) {
		create := gamev1alpha1.CapabilityBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: namespace,
				Labels: map[string]string{
					labelManagedBy: managedByCapabilityResolver,
					labelWorldName: world.Name,
					labelGameName:  gameName,
				},
			},
			Spec: spec,
		}
		// Ensure spec.worldRef is always set.
		create.Spec.WorldRef = &gamev1alpha1.WorldRef{Name: world.Name}
		if shard != nil {
			if create.Labels == nil {
				create.Labels = map[string]string{}
			}
			create.Labels[labelShardID] = fmt.Sprintf("%d", shard.Spec.ShardID)
			if err := controllerutil.SetControllerReference(shard, &create, r.Scheme); err != nil {
				return false, false, err
			}
			if err := r.Create(ctx, &create); err != nil {
				return false, false, err
			}
			return true, false, nil
		}
		if err := controllerutil.SetControllerReference(&world, &create, r.Scheme); err != nil {
			return false, false, err
		}
		if err := r.Create(ctx, &create); err != nil {
			return false, false, err
		}
		return true, false, nil
	}
	if err != nil {
		return false, false, err
	}

	before := obj.DeepCopy()
	if obj.Labels == nil {
		obj.Labels = map[string]string{}
	}
	obj.Labels[labelManagedBy] = managedByCapabilityResolver
	obj.Labels[labelWorldName] = world.Name
	obj.Labels[labelGameName] = gameName
	if shard != nil {
		obj.Labels[labelShardID] = fmt.Sprintf("%d", shard.Spec.ShardID)
	}
	obj.Spec = spec
	obj.Spec.WorldRef = &gamev1alpha1.WorldRef{Name: world.Name}
	if shard != nil {
		if err := controllerutil.SetControllerReference(shard, obj, r.Scheme); err != nil {
			return false, false, err
		}
	} else {
		if err := controllerutil.SetControllerReference(&world, obj, r.Scheme); err != nil {
			return false, false, err
		}
	}
	if err := r.Patch(ctx, obj, client.MergeFrom(before)); err != nil {
		return false, false, err
	}
	return false, true, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
