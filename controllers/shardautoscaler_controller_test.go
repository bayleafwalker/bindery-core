package controllers

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	binderyv1alpha1 "github.com/bayleafwalker/bindery-core/api/v1alpha1"
)

func TestShardAutoscaler_Reconcile(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = binderyv1alpha1.AddToScheme(scheme)

	world := &binderyv1alpha1.WorldInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"},
		Spec:       binderyv1alpha1.WorldInstanceSpec{WorldID: "w1", ShardCount: 2},
	}

	sa := &binderyv1alpha1.ShardAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "sa-1", Namespace: "default"},
		Spec: binderyv1alpha1.ShardAutoscalerSpec{
			WorldRef:  binderyv1alpha1.ObjectRef{Name: "world-1"},
			MinShards: 1,
			MaxShards: 5,
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world, sa).WithStatusSubresource(sa).Build()

	// MetricsClient is nil, so it should just clamp to Min/Max (which is satisfied)
	r := &ShardAutoscalerReconciler{Client: cl, Scheme: scheme, MetricsClient: nil}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "sa-1"}})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	var updatedSA binderyv1alpha1.ShardAutoscaler
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "default", Name: "sa-1"}, &updatedSA); err != nil {
		t.Fatalf("Get SA failed: %v", err)
	}

	if updatedSA.Status.CurrentShards != 2 {
		t.Errorf("Expected CurrentShards 2, got %d", updatedSA.Status.CurrentShards)
	}
	if updatedSA.Status.DesiredShards != 2 {
		t.Errorf("Expected DesiredShards 2, got %d", updatedSA.Status.DesiredShards)
	}
}

func TestShardAutoscaler_ScalesToMin(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = binderyv1alpha1.AddToScheme(scheme)

	world := &binderyv1alpha1.WorldInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"},
		Spec:       binderyv1alpha1.WorldInstanceSpec{WorldID: "w1", ShardCount: 1},
	}

	sa := &binderyv1alpha1.ShardAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "sa-1", Namespace: "default"},
		Spec: binderyv1alpha1.ShardAutoscalerSpec{
			WorldRef:  binderyv1alpha1.ObjectRef{Name: "world-1"},
			MinShards: 3,
			MaxShards: 5,
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world, sa).WithStatusSubresource(sa).Build()

	r := &ShardAutoscalerReconciler{Client: cl, Scheme: scheme, MetricsClient: nil}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "sa-1"}})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	var updatedWorld binderyv1alpha1.WorldInstance
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "default", Name: "world-1"}, &updatedWorld); err != nil {
		t.Fatalf("Get World failed: %v", err)
	}

	if updatedWorld.Spec.ShardCount != 3 {
		t.Errorf("Expected World ShardCount scaled to 3, got %d", updatedWorld.Spec.ShardCount)
	}
}

// The tests above all run with MetricsClient nil, which exercises only the
// min/max clamp. The metric-driven path -- calculateReplicaCount, the pod
// request lookup, and the utilisation ratio -- had no coverage at all, so the
// only scaling ever proven was "raise to minShards".
//
// These tests drive that path with a fake metrics clientset, which keeps them
// deterministic and needs no metrics-server.

// shardAutoscalerFixture builds a world, an autoscaler, matching pods with CPU
// requests, and the PodMetrics the controller will aggregate.
func shardAutoscalerFixture(t *testing.T, shardCount, min, max int32, target int32, podRequestMilli, podUsageMilli int64, podCount int) (*ShardAutoscalerReconciler, client.Client) {
	t.Helper()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = binderyv1alpha1.AddToScheme(scheme)

	world := &binderyv1alpha1.WorldInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"},
		Spec:       binderyv1alpha1.WorldInstanceSpec{WorldID: "w1", ShardCount: shardCount},
	}
	sa := &binderyv1alpha1.ShardAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "sa-1", Namespace: "default"},
		Spec: binderyv1alpha1.ShardAutoscalerSpec{
			WorldRef:  binderyv1alpha1.ObjectRef{Name: "world-1"},
			MinShards: min,
			MaxShards: max,
			Metrics: []binderyv1alpha1.MetricSpec{{
				Type: "Resource",
				Resource: &binderyv1alpha1.ResourceMetricSource{
					Name:                     "cpu",
					TargetAverageUtilization: &target,
				},
			}},
		},
	}

	objs := []client.Object{world, sa}
	var podMetrics []*metricsv1beta1.PodMetrics
	for i := 0; i < podCount; i++ {
		name := fmt.Sprintf("rt-world-1-shard-%d", i)
		labels := map[string]string{rtLabelWorldName: "world-1"}

		objs = append(objs, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: labels},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "app",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: *resource.NewMilliQuantity(podRequestMilli, resource.DecimalSI),
					},
				},
			}}},
		})

		podMetrics = append(podMetrics, &metricsv1beta1.PodMetrics{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: labels},
			Containers: []metricsv1beta1.ContainerMetrics{{
				Name: "app",
				Usage: corev1.ResourceList{
					corev1.ResourceCPU: *resource.NewMilliQuantity(podUsageMilli, resource.DecimalSI),
				},
			}},
		})
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(sa).Build()

	// Register PodMetrics through the tracker under the GVR the generated fake
	// actually queries. Passing them to NewSimpleClientset does NOT work: it
	// adds objects via UnsafeGuessKindToResource, which turns kind PodMetrics
	// into resource "podmetricses", while the generated client lists resource
	// "pods" (metrics.k8s.io/v1beta1 serves pod metrics under /pods). The GVRs
	// never match and every List silently returns empty -- which reads exactly
	// like "no metrics available" and would make these tests vacuously pass.
	metricsClient := metricsfake.NewSimpleClientset()
	podMetricsGVR := metricsv1beta1.SchemeGroupVersion.WithResource("pods")
	for _, pm := range podMetrics {
		if err := metricsClient.Tracker().Create(podMetricsGVR, pm, pm.Namespace); err != nil {
			t.Fatalf("seed PodMetrics %s: %v", pm.Name, err)
		}
	}

	r := &ShardAutoscalerReconciler{
		Client:        cl,
		Scheme:        scheme,
		MetricsClient: metricsClient,
	}
	return r, cl
}

func reconcileAutoscaler(t *testing.T, r *ShardAutoscalerReconciler) {
	t.Helper()

	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "sa-1"},
	}); err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
}

func worldShardCount(t *testing.T, cl client.Client) int32 {
	t.Helper()

	var world binderyv1alpha1.WorldInstance
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "world-1"}, &world); err != nil {
		t.Fatalf("Get World failed: %v", err)
	}
	return world.Spec.ShardCount
}

func TestShardAutoscaler_MetricsScaleUp(t *testing.T) {
	// 2 shards, pods running at their full request against a 50% target:
	// utilisation 100%, ratio 100/50 = 2, desired = ceil(2 * 2) = 4.
	r, cl := shardAutoscalerFixture(t, 2, 1, 10, 50, 100, 100, 2)
	reconcileAutoscaler(t, r)

	if got := worldShardCount(t, cl); got != 4 {
		t.Errorf("ShardCount = %d, want 4", got)
	}
}

func TestShardAutoscaler_MetricsScaleDown(t *testing.T) {
	// 4 shards barely used: 20m against a 100m request is 20% utilisation, and
	// against an 80% target the ratio is 0.25, so desired = ceil(4 * 0.25) = 1.
	//
	// This is the case minShards cannot express: the clamp only ever raises to
	// min, so without metrics a world that has grown never shrinks.
	r, cl := shardAutoscalerFixture(t, 4, 1, 10, 80, 100, 20, 4)
	reconcileAutoscaler(t, r)

	if got := worldShardCount(t, cl); got != 1 {
		t.Errorf("ShardCount = %d, want 1", got)
	}
}

func TestShardAutoscaler_MetricsScaleDownRespectsMin(t *testing.T) {
	// Same low utilisation, but minShards is a floor the metrics cannot cross.
	r, cl := shardAutoscalerFixture(t, 4, 3, 10, 80, 100, 20, 4)
	reconcileAutoscaler(t, r)

	if got := worldShardCount(t, cl); got != 3 {
		t.Errorf("ShardCount = %d, want 3 (clamped to minShards)", got)
	}
}

func TestShardAutoscaler_MetricsScaleUpRespectsMax(t *testing.T) {
	// Ratio wants 4 shards; maxShards caps the world at 3.
	r, cl := shardAutoscalerFixture(t, 2, 1, 3, 50, 100, 100, 2)
	reconcileAutoscaler(t, r)

	if got := worldShardCount(t, cl); got != 3 {
		t.Errorf("ShardCount = %d, want 3 (clamped to maxShards)", got)
	}
}

func TestShardAutoscaler_NoPodMetricsHoldsSteady(t *testing.T) {
	// No pods and no metrics: the controller must leave the world alone rather
	// than treating "no data" as "no load" and scaling to the floor.
	r, cl := shardAutoscalerFixture(t, 3, 1, 10, 50, 100, 100, 0)
	reconcileAutoscaler(t, r)

	if got := worldShardCount(t, cl); got != 3 {
		t.Errorf("ShardCount = %d, want 3 (unchanged with no metrics)", got)
	}
}

func TestShardAutoscaler_PodsWithoutRequestsHoldSteady(t *testing.T) {
	// Utilisation is a percentage OF the request. Pods with no CPU request give
	// no denominator, so they must be skipped rather than divided by zero.
	r, cl := shardAutoscalerFixture(t, 3, 1, 10, 50, 0, 100, 2)
	reconcileAutoscaler(t, r)

	if got := worldShardCount(t, cl); got != 3 {
		t.Errorf("ShardCount = %d, want 3 (unchanged when requests are unset)", got)
	}
}

func TestShardAutoscaler_StatusLagsOnePass(t *testing.T) {
	// status.currentShards is written from the count observed at the START of a
	// reconcile and the WorldInstance is updated afterwards, so the pass that
	// scales reports the OLD current against the new desired. Only the next
	// pass reports a settled pair. The e2e sharding phase polls for this reason.
	//
	// Scale-down is used because it reaches a fixed point with a static
	// fixture: at 20% utilisation against an 80% target the ratio is 0.25, so
	// once the world is at 1 shard, ceil(1 * 0.25) clamps back to 1.
	r, cl := shardAutoscalerFixture(t, 4, 1, 10, 80, 100, 20, 4)

	get := func() binderyv1alpha1.ShardAutoscaler {
		t.Helper()
		var sa binderyv1alpha1.ShardAutoscaler
		if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "sa-1"}, &sa); err != nil {
			t.Fatalf("Get SA failed: %v", err)
		}
		return sa
	}

	reconcileAutoscaler(t, r)
	sa := get()
	if sa.Status.CurrentShards != 4 || sa.Status.DesiredShards != 1 {
		t.Errorf("status = %d/%d, want 4/1 on the scaling pass", sa.Status.CurrentShards, sa.Status.DesiredShards)
	}
	if sa.Status.LastScaleTime == nil {
		t.Error("LastScaleTime not set on a pass that changed the shard count")
	}

	reconcileAutoscaler(t, r)
	sa = get()
	if sa.Status.CurrentShards != 1 || sa.Status.DesiredShards != 1 {
		t.Errorf("status = %d/%d, want 1/1 once settled", sa.Status.CurrentShards, sa.Status.DesiredShards)
	}
}

func TestShardAutoscaler_SustainedLoadKeepsGrowing(t *testing.T) {
	// A static fixture never relieves pressure, so a world held at 100%
	// utilisation against a 50% target keeps doubling: 2 -> 4 -> 8. This is the
	// controller working as designed -- real shards would absorb load and lower
	// the measured utilisation -- but it means the autoscaler has no damping or
	// hysteresis of its own, and maxShards is the only thing that stops it.
	r, cl := shardAutoscalerFixture(t, 2, 1, 100, 50, 100, 100, 2)

	reconcileAutoscaler(t, r)
	if got := worldShardCount(t, cl); got != 4 {
		t.Fatalf("after first pass ShardCount = %d, want 4", got)
	}
	reconcileAutoscaler(t, r)
	if got := worldShardCount(t, cl); got != 8 {
		t.Errorf("after second pass ShardCount = %d, want 8", got)
	}
}
