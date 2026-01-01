package controllers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	binderyv1alpha1 "github.com/bayleafwalker/bindery-core/api/v1alpha1"
)

func TestIntegration_StorageOrchestrator_ClientTier_SetsExternalStatusAndNoPVC(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short")
	}
	if os.Getenv("BINDERY_INTEGRATION") != "1" {
		t.Skip("set BINDERY_INTEGRATION=1 (or run `make test-integration`) to enable envtest integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	crdDir := filepath.Join("..", "k8s", "crds")
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{crdDir},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest (set KUBEBUILDER_ASSETS; try `make test-integration`): %v", err)
	}
	defer func() {
		_ = testEnv.Stop()
	}()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(client-go): %v", err)
	}
	if err := binderyv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(game): %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core): %v", err)
	}

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "bindery-integration"}}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	worldName := "world-1"
	shardName := "world-1-shard-0"
	claimName := "claim-client"

	claim := &binderyv1alpha1.WorldStorageClaim{
		TypeMeta:   metav1.TypeMeta{APIVersion: "bindery.platform/v1alpha1", Kind: "WorldStorageClaim"},
		ObjectMeta: metav1.ObjectMeta{Name: claimName, Namespace: ns.Name},
		Spec: binderyv1alpha1.WorldStorageClaimSpec{
			Scope:    binderyv1alpha1.WorldStorageScopeWorldShard,
			Tier:     binderyv1alpha1.WorldStorageTierClientLowLatency,
			WorldRef: binderyv1alpha1.ObjectRef{Name: worldName},
			ShardRef: &binderyv1alpha1.ObjectRef{Name: shardName},
			Size:     "1Gi",
			AccessModes: []string{
				"ReadWriteOnce",
			},
		},
	}
	if err := k8sClient.Create(ctx, claim); err != nil {
		t.Fatalf("create claim: %v", err)
	}

	r := &StorageOrchestratorReconciler{Client: k8sClient, Scheme: scheme}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns.Name, Name: claimName}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	expectedPVC := stablePVCName(worldName, shardName, string(binderyv1alpha1.WorldStorageTierClientLowLatency))

	if err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		var got binderyv1alpha1.WorldStorageClaim
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: claimName}, &got); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		if got.Status.Phase != "External" {
			return false, nil
		}
		if got.Status.ExternalURI == "" {
			return false, nil
		}
		if !strings.Contains(got.Status.ExternalURI, "/worlds/"+worldName) {
			return false, nil
		}
		if !strings.Contains(got.Status.ExternalURI, "/shards/"+shardName) {
			return false, nil
		}
		if got.Status.ClaimName != "" {
			return false, nil
		}

		var pvc corev1.PersistentVolumeClaim
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: expectedPVC}, &pvc)
		if err == nil {
			// Client-tier storage is external: PVC must not exist.
			return false, nil
		}
		return client.IgnoreNotFound(err) == nil, nil
	}); err != nil {
		t.Fatalf("timed out waiting for External status and verifying no PVC: %v", err)
	}
}
