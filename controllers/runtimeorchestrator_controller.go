package controllers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
)

const (
	rtLabelManagedBy = "game.platform/managed-by"
	rtLabelWorldName = "game.platform/world"
	rtLabelModule    = "game.platform/module"

	rtManagedBy = "runtimeorchestrator"

	annRuntimeImage = "anvil.dev/runtime-image"
	annRuntimePort  = "anvil.dev/runtime-port"
)

var rtNonDNS = regexp.MustCompile(`[^a-z0-9-]+`)

// RuntimeOrchestratorReconciler materializes runnable Kubernetes workloads for server-owned modules.
//
// MVP behavior:
// - Reconcile is driven by CapabilityBinding.
// - For bindings whose provider ModuleManifest declares runtime annotations, ensure a Deployment + Service exists.
// - Patch CapabilityBinding.status.provider.endpoint to point to the created Service.
//
// Client-side modules are out of scope here (no workloads created).
//
// RBAC:
// +kubebuilder:rbac:groups=game.platform,resources=capabilitybindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=game.platform,resources=capabilitybindings/status,verbs=update;patch
// +kubebuilder:rbac:groups=game.platform,resources=worldinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=game.platform,resources=modulemanifests,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
type RuntimeOrchestratorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *RuntimeOrchestratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		"controller", "RuntimeOrchestrator",
		"namespace", req.Namespace,
		"name", req.Name,
	)

	var binding gamev1alpha1.CapabilityBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Only orchestrate bindings that are scoped to a world.
	if binding.Spec.WorldRef == nil || binding.Spec.WorldRef.Name == "" {
		return ctrl.Result{}, nil
	}

	// Load owning world (needed for ownerRefs/labels).
	var world gamev1alpha1.WorldInstance
	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: binding.Spec.WorldRef.Name}, &world); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	providerName := binding.Spec.Provider.ModuleManifestName
	if providerName == "" {
		return ctrl.Result{}, nil
	}

	var providerMM gamev1alpha1.ModuleManifest
	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: providerName}, &providerMM); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	image := strings.TrimSpace(providerMM.Annotations[annRuntimeImage])
	if image == "" {
		// Convention: no runtime annotations means "not server-orchestrated".
		return ctrl.Result{}, nil
	}

	port := int32(50051)
	if raw := strings.TrimSpace(providerMM.Annotations[annRuntimePort]); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 && p <= 65535 {
			port = int32(p)
		}
	}

	// Stable names shared by Service and Deployment.
	workloadName := rtName(world.Name, providerName)
	labels := map[string]string{
		rtLabelManagedBy: rtManagedBy,
		rtLabelWorldName: world.Name,
		rtLabelModule:    providerName,
	}

	// 1) Ensure Service
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: workloadName, Namespace: req.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		service.Labels = mergeLabels(service.Labels, labels)
		service.Spec.Selector = mergeLabels(service.Spec.Selector, labels)
		service.Spec.Type = corev1.ServiceTypeClusterIP
		service.Spec.Ports = []corev1.ServicePort{{
			Name:       "grpc",
			Port:       port,
			TargetPort: intstrFromInt32(port),
			Protocol:   corev1.ProtocolTCP,
		}}
		return controllerutil.SetControllerReference(&world, service, r.Scheme)
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	// 2) Ensure Deployment
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: workloadName, Namespace: req.Namespace}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		deployment.Labels = mergeLabels(deployment.Labels, labels)
		deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}
		deployment.Spec.Replicas = int32Ptr(1)
		deployment.Spec.Template.ObjectMeta.Labels = mergeLabels(deployment.Spec.Template.ObjectMeta.Labels, labels)
		deployment.Spec.Template.Spec.Containers = []corev1.Container{{
			Name:  "module",
			Image: image,
			Args:  []string{"sleep", "365d"},
			Ports: []corev1.ContainerPort{{ContainerPort: port, Name: "grpc"}},
		}}
		return controllerutil.SetControllerReference(&world, deployment, r.Scheme)
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	// 3) Publish the endpoint back onto the binding status.
	desiredEndpoint := &gamev1alpha1.EndpointRef{
		Type:  "kubernetesService",
		Value: workloadName,
		Port:  port,
	}
	if binding.Status.Provider == nil || binding.Status.Provider.Endpoint == nil ||
		binding.Status.Provider.Endpoint.Type != desiredEndpoint.Type ||
		binding.Status.Provider.Endpoint.Value != desiredEndpoint.Value ||
		binding.Status.Provider.Endpoint.Port != desiredEndpoint.Port {
		before := binding.DeepCopy()
		binding.Status.Provider = &gamev1alpha1.ProviderStatus{Endpoint: desiredEndpoint}
		if err := r.Status().Patch(ctx, &binding, client.MergeFrom(before)); err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.Info("ensured runtime", "providerModule", providerName, "service", workloadName, "image", image, "port", port)
	return ctrl.Result{}, nil
}

func (r *RuntimeOrchestratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gamev1alpha1.CapabilityBinding{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}

func mergeLabels(dst, src map[string]string) map[string]string {
	if dst == nil {
		dst = map[string]string{}
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func int32Ptr(v int32) *int32 { return &v }

func intstrFromInt32(v int32) intstr.IntOrString {
	return intstr.FromInt(int(v))
}

func rtName(worldName, moduleName string) string {
	base := fmt.Sprintf("rt-%s-%s", worldName, moduleName)
	base = strings.ToLower(base)
	base = strings.ReplaceAll(base, ".", "-")
	base = rtNonDNS.ReplaceAllString(base, "-")
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
