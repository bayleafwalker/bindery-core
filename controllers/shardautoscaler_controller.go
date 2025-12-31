package controllers

import (
	"context"
	"fmt"
	"math"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	metrics "k8s.io/metrics/pkg/client/clientset/versioned"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
)

// ShardAutoscalerReconciler reconciles a ShardAutoscaler object
type ShardAutoscalerReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	MetricsClient metrics.Interface
}

//+kubebuilder:rbac:groups=game.platform,resources=shardautoscalers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=game.platform,resources=shardautoscalers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=game.platform,resources=worldinstances,verbs=get;list;watch;update;patch

func (r *ShardAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var sa gamev1alpha1.ShardAutoscaler
	if err := r.Get(ctx, req.NamespacedName, &sa); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch WorldInstance
	var world gamev1alpha1.WorldInstance
	if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: sa.Spec.WorldRef.Name}, &world); err != nil {
		logger.Error(err, "unable to fetch WorldInstance")
		return ctrl.Result{}, err
	}

	currentShards := world.Spec.ShardCount
	if currentShards == 0 {
		currentShards = 1
	}

	// Calculate desired shards based on metrics
	calculatedShards := currentShards
	if r.MetricsClient != nil {
		var maxDesired int32 = 0
		for _, metric := range sa.Spec.Metrics {
			if metric.Type == "Resource" && metric.Resource != nil {
				desired, err := r.calculateReplicaCount(ctx, req.Namespace, world.Name, currentShards, metric.Resource)
				if err != nil {
					logger.Error(err, "failed to calculate replica count", "metric", metric.Resource.Name)
					continue
				}
				if desired > maxDesired {
					maxDesired = desired
				}
			}
		}
		if maxDesired > 0 {
			calculatedShards = maxDesired
		}
	}

	// Clamp to Min/Max
	desiredShards := calculatedShards
	if desiredShards < sa.Spec.MinShards {
		desiredShards = sa.Spec.MinShards
	}
	if desiredShards > sa.Spec.MaxShards {
		desiredShards = sa.Spec.MaxShards
	}

	// Update Status
	sa.Status.CurrentShards = currentShards
	sa.Status.DesiredShards = desiredShards
	now := metav1.Now()
	if desiredShards != currentShards {
		sa.Status.LastScaleTime = &now
	}

	if err := r.Status().Update(ctx, &sa); err != nil {
		logger.Error(err, "unable to update ShardAutoscaler status")
		return ctrl.Result{}, err
	}

	// Update WorldInstance if needed
	// Simple hysteresis: only scale if difference is significant or we are out of bounds
	// For now, we just scale directly.
	if desiredShards != currentShards {
		logger.Info("Scaling world shards", "current", currentShards, "desired", desiredShards)
		world.Spec.ShardCount = desiredShards
		if err := r.Update(ctx, &world); err != nil {
			logger.Error(err, "unable to update WorldInstance")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *ShardAutoscalerReconciler) calculateReplicaCount(ctx context.Context, namespace, worldName string, currentShards int32, resource *gamev1alpha1.ResourceMetricSource) (int32, error) {
	if resource.TargetAverageUtilization == nil {
		return currentShards, nil
	}

	// List PodMetrics for the world
	labelSelector := fmt.Sprintf("%s=%s", rtLabelWorldName, worldName)
	podMetricsList, err := r.MetricsClient.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return 0, err
	}

	if len(podMetricsList.Items) == 0 {
		return currentShards, nil
	}

	var totalUsage int64
	var totalRequest int64 // We need requests to calculate utilization %

	// Note: In a real implementation, we would need to fetch the Pods to get their resource requests.
	// For this simplified implementation, we will assume a fixed request or just use raw usage if target is raw value.
	// But the API says TargetAverageUtilization (percentage).
	// So we MUST fetch pods to get requests.
	// To keep it simple and avoid fetching all pods, let's assume we can get requests from the first pod we find (assuming homogeneity).
	// Or better, let's just sum up usage and assume the user provided a raw target?
	// The API spec says TargetAverageUtilization is int32 (percentage).

	// Let's fetch the pods to get requests.
	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.InNamespace(namespace), client.MatchingLabels{rtLabelWorldName: worldName}); err != nil {
		return 0, err
	}

	podRequests := make(map[string]int64) // podName -> request value
	for _, pod := range podList.Items {
		req := int64(0)
		for _, c := range pod.Spec.Containers {
			if resource.Name == "cpu" {
				req += c.Resources.Requests.Cpu().MilliValue()
			} else if resource.Name == "memory" {
				req += c.Resources.Requests.Memory().Value()
			}
		}
		podRequests[pod.Name] = req
	}

	count := 0
	for _, pm := range podMetricsList.Items {
		usage := int64(0)
		for _, c := range pm.Containers {
			if resource.Name == "cpu" {
				usage += c.Usage.Cpu().MilliValue()
			} else if resource.Name == "memory" {
				usage += c.Usage.Memory().Value()
			}
		}

		if req, ok := podRequests[pm.Name]; ok && req > 0 {
			totalUsage += usage
			totalRequest += req
			count++
		}
	}

	if count == 0 || totalRequest == 0 {
		return currentShards, nil
	}

	// avgUtilization = (totalUsage / totalRequest) * 100
	avgUtilization := (float64(totalUsage) / float64(totalRequest)) * 100
	targetUtilization := float64(*resource.TargetAverageUtilization)

	// desired = current * (avg / target)
	usageRatio := avgUtilization / targetUtilization
	desired := int32(math.Ceil(float64(currentShards) * usageRatio))

	return desired, nil
}

func (r *ShardAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gamev1alpha1.ShardAutoscaler{}).
		Complete(r)
}
