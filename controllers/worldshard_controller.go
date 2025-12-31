package controllers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	binderyv1alpha1 "github.com/bayleafwalker/bindery-core/api/v1alpha1"
)

const (
	labelShardID = "bindery.platform/shard"

	managedByWorldShardController = "worldshardcontroller"
)

// WorldShardReconciler materializes explicit WorldShard resources for a WorldInstance.
//
// RBAC:
// +kubebuilder:rbac:groups=bindery.platform,resources=worldinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=bindery.platform,resources=worldinstances/status,verbs=get
// +kubebuilder:rbac:groups=bindery.platform,resources=worldshards,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bindery.platform,resources=worldshards/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
type WorldShardReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *WorldShardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		"controller", "WorldShard",
		"namespace", req.Namespace,
		"world", req.Name,
	)

	var world binderyv1alpha1.WorldInstance
	if err := r.Get(ctx, req.NamespacedName, &world); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	shardCount := world.Spec.ShardCount
	if shardCount < 1 {
		shardCount = 1
	}

	var shards binderyv1alpha1.WorldShardList
	if err := r.List(ctx, &shards,
		client.InNamespace(req.Namespace),
		client.MatchingLabels{labelWorldName: world.Name, labelManagedBy: managedByWorldShardController},
	); err != nil {
		logger.Error(err, "failed to list worldshards")
		return ctrl.Result{}, err
	}

	// Create missing shards.
	created := 0
	for id := int32(0); id < shardCount; id++ {
		name := stableWorldShardName(world.Name, id)
		obj := &binderyv1alpha1.WorldShard{}
		err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: name}, obj)
		if apierrors.IsNotFound(err) {
			create := &binderyv1alpha1.WorldShard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: req.Namespace,
					Labels: map[string]string{
						labelManagedBy: managedByWorldShardController,
						labelWorldName: world.Name,
						labelGameName:  world.Spec.GameRef.Name,
						labelShardID:   fmt.Sprintf("%d", id),
					},
				},
				Spec: binderyv1alpha1.WorldShardSpec{
					WorldRef: binderyv1alpha1.ObjectRef{Name: world.Name},
					ShardID:  id,
				},
				Status: binderyv1alpha1.WorldShardStatus{Phase: "Ready"},
			}
			if err := controllerutil.SetControllerReference(&world, create, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, create); err != nil {
				logger.Error(err, "failed to create worldshard", "shard", id)
				return ctrl.Result{}, err
			}
			created++
			r.recordEventf(&world, "Normal", "WorldShardCreated", "Created shard %d", id)
			continue
		}
		if err != nil {
			logger.Error(err, "failed to get worldshard", "shard", id)
			return ctrl.Result{}, err
		}
	}

	// Scale down: delete shards with ShardID >= shardCount.
	deleted := 0
	// Sort deterministic deletion order.
	sort.Slice(shards.Items, func(i, j int) bool { return shards.Items[i].Spec.ShardID < shards.Items[j].Spec.ShardID })
	for i := range shards.Items {
		s := &shards.Items[i]
		if s.Spec.ShardID < shardCount {
			continue
		}
		if err := r.Delete(ctx, s); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "failed to delete worldshard", "shard", s.Spec.ShardID)
			return ctrl.Result{}, err
		}
		deleted++
	}

	if created > 0 || deleted > 0 {
		logger.Info("reconciled shards", "shardCount", shardCount, "created", created, "deleted", deleted)
	}

	return ctrl.Result{}, nil
}

func (r *WorldShardReconciler) recordEventf(obj client.Object, eventType, reason, messageFmt string, args ...any) {
	if r.Recorder == nil || obj == nil {
		return
	}
	r.Recorder.Eventf(obj, eventType, reason, messageFmt, args...)
}

func (r *WorldShardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&binderyv1alpha1.WorldInstance{}).
		Owns(&binderyv1alpha1.WorldShard{}).
		Complete(r)
}

func stableWorldShardName(worldName string, shardID int32) string {
	base := fmt.Sprintf("ws-%s-%04d", worldName, shardID)
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
