package controllers

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
)

func TestStorageOrchestrator_ServerTierCreatesPVCAndStatus(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	if err := gamev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(game): %v", err)
	}

	claim := &gamev1alpha1.WorldStorageClaim{
		TypeMeta:   metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "WorldStorageClaim"},
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "ns"},
		Spec: gamev1alpha1.WorldStorageClaimSpec{
			Scope:       gamev1alpha1.WorldStorageScopeWorld,
			Tier:        gamev1alpha1.WorldStorageTierServerLowLatency,
			WorldRef:    gamev1alpha1.ObjectRef{Name: "w1"},
			Size:        "1Gi",
			AccessModes: []string{"ReadWriteOnce"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(claim).WithStatusSubresource(claim).Build()

	r := &StorageOrchestratorReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "c1"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got gamev1alpha1.WorldStorageClaim
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "c1"}, &got); err != nil {
		t.Fatalf("Get claim: %v", err)
	}
	if got.Status.ClaimName == "" {
		t.Fatalf("expected status.claimName")
	}

	var pvc corev1.PersistentVolumeClaim
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "ns", Name: got.Status.ClaimName}, &pvc); err != nil {
		t.Fatalf("expected PVC: %v", err)
	}
}

func TestStorageOrchestrator_ClientTierSetsExternalStatus(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	if err := gamev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(game): %v", err)
	}

	claim := &gamev1alpha1.WorldStorageClaim{
		TypeMeta:   metav1.TypeMeta{APIVersion: "game.platform/v1alpha1", Kind: "WorldStorageClaim"},
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "ns"},
		Spec: gamev1alpha1.WorldStorageClaimSpec{
			Scope:    gamev1alpha1.WorldStorageScopeWorld,
			Tier:     gamev1alpha1.WorldStorageTierClientLowLatency,
			WorldRef: gamev1alpha1.ObjectRef{Name: "w1"},
			Size:     "1Gi",
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(claim).WithStatusSubresource(claim).Build()

	r := &StorageOrchestratorReconciler{Client: cl, Scheme: scheme}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "c1"}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got gamev1alpha1.WorldStorageClaim
	if err := cl.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "c1"}, &got); err != nil {
		t.Fatalf("Get claim: %v", err)
	}
	if got.Status.Phase != "External" {
		t.Fatalf("expected External, got %q", got.Status.Phase)
	}
	if got.Status.ExternalURI == "" {
		t.Fatalf("expected externalUri")
	}
}
