package controllers

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
)

func TestShardAutoscaler_Reconcile(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = gamev1alpha1.AddToScheme(scheme)

	world := &gamev1alpha1.WorldInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"},
		Spec:       gamev1alpha1.WorldInstanceSpec{WorldID: "w1", ShardCount: 2},
	}

	sa := &gamev1alpha1.ShardAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "sa-1", Namespace: "default"},
		Spec: gamev1alpha1.ShardAutoscalerSpec{
			WorldRef:  gamev1alpha1.ObjectRef{Name: "world-1"},
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

	var updatedSA gamev1alpha1.ShardAutoscaler
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
	_ = gamev1alpha1.AddToScheme(scheme)

	world := &gamev1alpha1.WorldInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "world-1", Namespace: "default"},
		Spec:       gamev1alpha1.WorldInstanceSpec{WorldID: "w1", ShardCount: 1},
	}

	sa := &gamev1alpha1.ShardAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "sa-1", Namespace: "default"},
		Spec: gamev1alpha1.ShardAutoscalerSpec{
			WorldRef:  gamev1alpha1.ObjectRef{Name: "world-1"},
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

	var updatedWorld gamev1alpha1.WorldInstance
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "default", Name: "world-1"}, &updatedWorld); err != nil {
		t.Fatalf("Get World failed: %v", err)
	}

	if updatedWorld.Spec.ShardCount != 3 {
		t.Errorf("Expected World ShardCount scaled to 3, got %d", updatedWorld.Spec.ShardCount)
	}
}
