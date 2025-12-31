package controllers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	binderyv1alpha1 "github.com/bayleafwalker/bindery-core/api/v1alpha1"
)

// StorageOrchestratorReconciler materializes backing PVCs for WorldStorageClaims (server tiers).
//
// RBAC:
// +kubebuilder:rbac:groups=bindery.platform,resources=worldstorageclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bindery.platform,resources=worldstorageclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
type StorageOrchestratorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *StorageOrchestratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		"controller", "StorageOrchestrator",
		"namespace", req.Namespace,
		"claim", req.Name,
	)

	var claim binderyv1alpha1.WorldStorageClaim
	if err := r.Get(ctx, req.NamespacedName, &claim); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger = logger.WithValues(
		"scope", claim.Spec.Scope,
		"tier", claim.Spec.Tier,
		"world", claim.Spec.WorldRef.Name,
	)
	if claim.Spec.ShardRef != nil {
		logger = logger.WithValues("shard", claim.Spec.ShardRef.Name)
	}

	// Client-side tiers are external to the cluster: no PVC.
	if claim.Spec.Tier == binderyv1alpha1.WorldStorageTierClientLowLatency {
		before := claim.DeepCopy()
		claim.Status.Phase = "External"
		claim.Status.Message = "Client-side low-latency storage is outside the Kubernetes cluster"
		claim.Status.ClaimName = ""
		claim.Status.StorageClassName = ""
		claim.Status.ExternalURI = defaultClientStorageURI(claim.Spec.WorldRef.Name, shardRefName(claim.Spec.ShardRef))
		if err := r.Status().Patch(ctx, &claim, client.MergeFrom(before)); err != nil {
			logger.Error(err, "failed to patch claim status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Server-side tiers => PVC.
	requestedSC := strings.TrimSpace(claim.Spec.StorageClassName)
	if requestedSC == "" {
		requestedSC = defaultStorageClassForTier(claim.Spec.Tier)
	}

	sizeQty, err := resource.ParseQuantity(strings.TrimSpace(claim.Spec.Size))
	if err != nil {
		before := claim.DeepCopy()
		claim.Status.Phase = "Error"
		claim.Status.Message = fmt.Sprintf("InvalidSize: %v", err)
		_ = r.Status().Patch(ctx, &claim, client.MergeFrom(before))
		r.recordEventf(&claim, "Warning", "InvalidSize", "Invalid size %q: %v", claim.Spec.Size, err)
		return ctrl.Result{}, nil
	}

	accessModes := claim.Spec.AccessModes
	if len(accessModes) == 0 {
		accessModes = []string{"ReadWriteOnce"}
	}
	modeObjs := make([]corev1.PersistentVolumeAccessMode, 0, len(accessModes))
	for _, m := range accessModes {
		modeObjs = append(modeObjs, corev1.PersistentVolumeAccessMode(m))
	}

	pvcName := stablePVCName(claim.Spec.WorldRef.Name, shardRefName(claim.Spec.ShardRef), string(claim.Spec.Tier))
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: req.Namespace}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, pvc, func() error {
		if pvc.Labels == nil {
			pvc.Labels = map[string]string{}
		}
		pvc.Labels[labelWorldName] = claim.Spec.WorldRef.Name
		if claim.Labels != nil {
			if shardID, ok := claim.Labels[labelShardID]; ok && strings.TrimSpace(shardID) != "" {
				pvc.Labels[labelShardID] = strings.TrimSpace(shardID)
			}
		}
		if claim.Spec.ShardRef != nil && pvc.Labels[labelShardID] == "" {
			pvc.Labels[labelShardID] = claim.Spec.ShardRef.Name
		}
		pvc.Labels["bindery.platform/storage-tier"] = string(claim.Spec.Tier)
		pvc.Spec.AccessModes = modeObjs
		pvc.Spec.Resources.Requests = corev1.ResourceList{corev1.ResourceStorage: sizeQty}
		if strings.TrimSpace(requestedSC) != "" {
			pvc.Spec.StorageClassName = &requestedSC
		}
		return controllerutil.SetControllerReference(&claim, pvc, r.Scheme)
	})
	if err != nil {
		logger.Error(err, "failed to ensure pvc", "pvc", pvcName)
		r.recordEventf(&claim, "Warning", "EnsurePVCFailed", "Failed to ensure PVC %q: %v", pvcName, err)
		return ctrl.Result{}, err
	}

	before := claim.DeepCopy()
	claim.Status.ClaimName = pvcName
	claim.Status.StorageClassName = requestedSC
	if pvc.Status.Phase == corev1.ClaimBound {
		claim.Status.Phase = "Bound"
		claim.Status.Message = "PVC bound"
	} else {
		claim.Status.Phase = "Pending"
		claim.Status.Message = "PVC pending"
	}
	if err := r.Status().Patch(ctx, &claim, client.MergeFrom(before)); err != nil {
		logger.Error(err, "failed to patch claim status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *StorageOrchestratorReconciler) recordEventf(obj client.Object, eventType, reason, messageFmt string, args ...any) {
	if r.Recorder == nil || obj == nil {
		return
	}
	r.Recorder.Eventf(obj, eventType, reason, messageFmt, args...)
}

func (r *StorageOrchestratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&binderyv1alpha1.WorldStorageClaim{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}

func defaultStorageClassForTier(tier binderyv1alpha1.WorldStorageTier) string {
	// Defaults are resolved via env vars so clusters can configure without CRD changes.
	switch tier {
	case binderyv1alpha1.WorldStorageTierServerLowLatency:
		return strings.TrimSpace(os.Getenv("BINDERY_STORAGECLASS_SERVER_LOW_LATENCY"))
	case binderyv1alpha1.WorldStorageTierServerHighLatency:
		return strings.TrimSpace(os.Getenv("BINDERY_STORAGECLASS_SERVER_HIGH_LATENCY"))
	default:
		return strings.TrimSpace(os.Getenv("BINDERY_STORAGECLASS_DEFAULT"))
	}
}

func defaultClientStorageURI(worldName, shardName string) string {
	if shardName == "" {
		return fmt.Sprintf("file://$HOME/.bindery/worlds/%s", worldName)
	}
	return fmt.Sprintf("file://$HOME/.bindery/worlds/%s/shards/%s", worldName, shardName)
}

func shardRefName(ref *binderyv1alpha1.ObjectRef) string {
	if ref == nil {
		return ""
	}
	return ref.Name
}

func stablePVCName(worldName, shard, tier string) string {
	base := fmt.Sprintf("pvc-%s-%s-%s", worldName, shard, tier)
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
