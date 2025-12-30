package controllers

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
)

func TestWorldShardController_CreatesShardsForWorld(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	if err := gamev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(game): %v", err)
	}

	world := &gamev1alpha1.WorldInstance{
		TypeMeta:   metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "WorldInstance"},
		ObjectMeta: metav1.ObjectMeta{Name: "w1", Namespace: "ns"},
		Spec: gamev1alpha1.WorldInstanceSpec{
			GameRef:      gamev1alpha1.ObjectRef{Name: "g"},
			WorldID:      "world-1",
			Region:       "r",
			ShardCount:   3,
			DesiredState: "Running",
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(world).Build()

	r := &WorldShardReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "w1"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var list gamev1alpha1.WorldShardList
	if err := cl.List(ctx, &list, client.InNamespace("ns")); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Items) != 3 {
		t.Fatalf("expected 3 shards, got %d", len(list.Items))
	}
}
