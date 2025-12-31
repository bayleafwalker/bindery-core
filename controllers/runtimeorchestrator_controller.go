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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
)

const (
	rtLabelManagedBy = "game.platform/managed-by"
	rtLabelWorldName = "game.platform/world"
	rtLabelModule    = "game.platform/module"

	rtManagedBy = "runtimeorchestrator"

	idxBindingConsumer = "spec.consumer.moduleManifestName"
	idxBindingProvider = "spec.provider.moduleManifestName"

	annRuntimeImage = "anvil.dev/runtime-image"
	annRuntimePort  = "anvil.dev/runtime-port"

	annStorageTier        = "anvil.dev/storage-tier"
	annStorageSize        = "anvil.dev/storage-size"
	annStorageScope       = "anvil.dev/storage-scope"
	annStorageAccessModes = "anvil.dev/storage-access-modes"
	annStorageMountPath   = "anvil.dev/storage-mount-path"
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
// +kubebuilder:rbac:groups=game.platform,resources=worldshards,verbs=get;list;watch
// +kubebuilder:rbac:groups=game.platform,resources=modulemanifests,verbs=get;list;watch
// +kubebuilder:rbac:groups=game.platform,resources=worldstorageclaims,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=game.platform,resources=worldstorageclaims/status,verbs=get
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
type RuntimeOrchestratorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *RuntimeOrchestratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	anvilControllerReconcileTotal.WithLabelValues("RuntimeOrchestrator").Inc()

	logger := log.FromContext(ctx).WithValues(
		"controller", "RuntimeOrchestrator",
		"namespace", req.Namespace,
		"binding", req.Name,
	)

	var binding gamev1alpha1.CapabilityBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		anvilControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
		return ctrl.Result{}, err
	}

	logger = logger.WithValues(
		"capabilityId", binding.Spec.CapabilityID,
		"consumerModule", binding.Spec.Consumer.ModuleManifestName,
		"providerModule", binding.Spec.Provider.ModuleManifestName,
	)

	shardLabel := ""
	if binding.Labels != nil {
		shardLabel = strings.TrimSpace(binding.Labels[labelShardID])
	}
	if shardLabel != "" {
		logger = logger.WithValues("shard", shardLabel)
	}

	// Only orchestrate bindings that are scoped to a world.
	if binding.Spec.WorldRef == nil || binding.Spec.WorldRef.Name == "" {
		logger.V(1).Info("skipping binding without world")
		return ctrl.Result{}, nil
	}
	logger = logger.WithValues("world", binding.Spec.WorldRef.Name)

	// Load owning world (needed for ownerRefs/labels).
	var world gamev1alpha1.WorldInstance
	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: binding.Spec.WorldRef.Name}, &world); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("world not found; skipping")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to load world")
		r.recordEventf(&binding, "Warning", "GetWorldFailed", "Failed to get WorldInstance %q: %v", binding.Spec.WorldRef.Name, err)
		anvilControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
		return ctrl.Result{}, err
	}

	// Load GameDefinition for colocation logic
	var gameDef gamev1alpha1.GameDefinition
	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: world.Spec.GameRef.Name}, &gameDef); err != nil {
		logger.V(1).Info("game definition not found; proceeding without colocation logic", "game", world.Spec.GameRef.Name)
	}

	var shardObj *gamev1alpha1.WorldShard
	shardName := ""
	if shardLabel != "" {
		id, err := strconv.Atoi(shardLabel)
		if err != nil || id < 0 {
			logger.V(1).Info("invalid shard label; treating as non-sharded binding", "labelValue", shardLabel)
			shardLabel = ""
		} else {
			shardName = stableWorldShardName(world.Name, int32(id))
			var ws gamev1alpha1.WorldShard
			if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: shardName}, &ws); err != nil {
				if apierrors.IsNotFound(err) {
					logger.V(1).Info("worldshard not found yet; requeue", "worldShard", shardName)
					return ctrl.Result{Requeue: true}, nil
				}
				logger.Error(err, "failed to get worldshard", "worldShard", shardName)
				anvilControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
				return ctrl.Result{}, err
			}
			shardObj = &ws
		}
	}

	providerName := binding.Spec.Provider.ModuleManifestName
	if providerName == "" {
		logger.V(1).Info("skipping binding without provider module")
		return ctrl.Result{}, nil
	}

	var providerMM gamev1alpha1.ModuleManifest
	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: providerName}, &providerMM); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("provider modulemanifest not found; skipping")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to load provider modulemanifest")
		r.recordEventf(&binding, "Warning", "GetProviderModuleFailed", "Failed to get provider ModuleManifest %q: %v", providerName, err)
		anvilControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
		return ctrl.Result{}, err
	}

	image := strings.TrimSpace(providerMM.Annotations[annRuntimeImage])
	if image == "" {
		// Convention: no runtime annotations means "not server-orchestrated".
		logger.V(1).Info("provider not server-orchestrated (missing runtime image annotation)")
		// Mark binding as runtime-ready (not applicable) for debuggability.
		before := binding.DeepCopy()
		binding.Status.ObservedGeneration = binding.Generation
		setBindingCondition(&binding, metav1.Condition{
			Type:    BindingConditionRuntimeReady,
			Status:  metav1.ConditionTrue,
			Reason:  "NotServerOrchestrated",
			Message: "Provider has no runtime annotations; no server workload will be created",
		})
		_ = r.Status().Patch(ctx, &binding, client.MergeFrom(before))
		return ctrl.Result{}, nil
	}

	port := int32(50051)
	if raw := strings.TrimSpace(providerMM.Annotations[annRuntimePort]); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 && p <= 65535 {
			port = int32(p)
		} else {
			logger.V(1).Info("invalid runtime port annotation; using default", "annotationValue", raw, "defaultPort", port)
		}
	}

	// Determine colocation
	colocGroup := getColocationGroup(&gameDef, providerName)
	isColocPod := colocGroup != nil && colocGroup.Strategy == "Pod"

	// Stable names shared by Service and Deployment.
	// For Service, we always use the module-specific name.
	serviceName := rtNameWithShard(world.Name, shardLabel, providerName)

	// For Deployment, we might use a shared name.
	deploymentName := rtNameWithShard(world.Name, shardLabel, providerName)
	if isColocPod {
		deploymentName = rtNameWithShard(world.Name, shardLabel, "coloc-"+colocGroup.Name)
	}

	labels := map[string]string{
		rtLabelManagedBy: rtManagedBy,
		rtLabelWorldName: world.Name,
	}
	if shardLabel != "" {
		labels[labelShardID] = shardLabel
	}

	// Service labels
	serviceLabels := mergeLabels(nil, labels)
	serviceLabels[rtLabelModule] = providerName
	if isColocPod {
		serviceLabels["game.platform/coloc-group"] = colocGroup.Name
	}

	// Deployment labels
	deploymentLabels := mergeLabels(nil, labels)
	if isColocPod {
		deploymentLabels["game.platform/coloc-group"] = colocGroup.Name
	} else {
		deploymentLabels[rtLabelModule] = providerName
	}

	// Optional persistent storage request driven by ModuleManifest annotations.
	storageTierRaw := strings.TrimSpace(providerMM.Annotations[annStorageTier])
	storageSize := strings.TrimSpace(providerMM.Annotations[annStorageSize])
	if storageSize == "" {
		storageSize = "1Gi"
	}
	storageMountPath := strings.TrimSpace(providerMM.Annotations[annStorageMountPath])
	if storageMountPath == "" {
		storageMountPath = "/var/anvil/state"
	}
	storageScopeRaw := strings.TrimSpace(providerMM.Annotations[annStorageScope])
	if storageScopeRaw == "" {
		if shardLabel != "" {
			storageScopeRaw = string(gamev1alpha1.WorldStorageScopeWorldShard)
		} else {
			storageScopeRaw = string(gamev1alpha1.WorldStorageScopeWorld)
		}
	}
	accessModes := parseCSV(providerMM.Annotations[annStorageAccessModes])

	var volumeToMount *corev1.Volume
	var mountToUse *corev1.VolumeMount
	if storageTierRaw != "" {
		tier := gamev1alpha1.WorldStorageTier(storageTierRaw)
		scope := gamev1alpha1.WorldStorageScope(storageScopeRaw)
		if tier == gamev1alpha1.WorldStorageTierServerLowLatency || tier == gamev1alpha1.WorldStorageTierServerHighLatency {
			// Ensure claim exists; StorageOrchestrator will materialize the PVC.
			claimName := stableWSCName(world.Name, shardName, storageTierRaw)
			if err := r.ensureWorldStorageClaim(ctx, req.Namespace, &world, shardObj, claimName, scope, tier, storageSize, accessModes, shardLabel, shardName); err != nil {
				logger.Error(err, "failed to ensure world storage claim", "claim", claimName)
				r.recordEventf(&binding, "Warning", "EnsureWorldStorageClaimFailed", "Failed to ensure WorldStorageClaim %q: %v", claimName, err)
				return ctrl.Result{}, err
			}
			pvcName := stablePVCName(world.Name, shardName, storageTierRaw)
			volumeToMount = &corev1.Volume{
				Name: "anvil-state",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName},
				},
			}
			mountToUse = &corev1.VolumeMount{Name: "anvil-state", MountPath: storageMountPath}
		} else {
			logger.V(1).Info("storage tier not supported for server workload; skipping", "tier", storageTierRaw)
		}
	}

	// 1) Ensure Service
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: req.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		service.Labels = mergeLabels(service.Labels, serviceLabels)

		// Selector logic
		selector := map[string]string{
			rtLabelManagedBy: rtManagedBy,
			rtLabelWorldName: world.Name,
		}
		if shardLabel != "" {
			selector[labelShardID] = shardLabel
		}
		if isColocPod {
			selector["game.platform/coloc-group"] = colocGroup.Name
		} else {
			selector[rtLabelModule] = providerName
		}
		service.Spec.Selector = selector

		service.Spec.Type = corev1.ServiceTypeClusterIP
		service.Spec.Ports = []corev1.ServicePort{{
			Name:       "grpc",
			Port:       port,
			TargetPort: intstrFromInt32(port),
			Protocol:   corev1.ProtocolTCP,
		}}
		if shardObj != nil {
			return controllerutil.SetControllerReference(shardObj, service, r.Scheme)
		}
		return controllerutil.SetControllerReference(&world, service, r.Scheme)
	})
	if err != nil {
		logger.Error(err, "failed to ensure service", "service", serviceName)
		r.recordEventf(&binding, "Warning", "EnsureServiceFailed", "Failed to ensure Service %q: %v", serviceName, err)
		anvilControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
		return ctrl.Result{}, err
	}

	// 2) Ensure Deployment
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: req.Namespace}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		deployment.Labels = mergeLabels(deployment.Labels, deploymentLabels)
		deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: deploymentLabels}
		deployment.Spec.Replicas = int32Ptr(1)
		deployment.Spec.Template.ObjectMeta.Labels = mergeLabels(deployment.Spec.Template.ObjectMeta.Labels, deploymentLabels)

		// Container logic
		containerName := "module"
		if isColocPod {
			containerName = providerName
		}

		container := corev1.Container{
			Name:  containerName,
			Image: image,
			Args:  []string{"sleep", "365d"},
			Ports: []corev1.ContainerPort{{ContainerPort: port, Name: "grpc"}},
		}
		if volumeToMount != nil && mountToUse != nil {
			container.VolumeMounts = append(container.VolumeMounts, *mountToUse)
		}

		// UDS Logic
		if isColocPod {
			// Add shared volume if not present
			hasSharedVol := false
			for _, v := range deployment.Spec.Template.Spec.Volumes {
				if v.Name == "shared-socket" {
					hasSharedVol = true
					break
				}
			}
			if !hasSharedVol {
				deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
					Name: "shared-socket",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				})
			}

			// Mount volume in container
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      "shared-socket",
				MountPath: "/var/run/anvil",
			})

			// Set Env Vars
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  "ANVIL_UDS_DIR",
				Value: "/var/run/anvil",
			})
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  "ANVIL_MODULE_NAME",
				Value: providerName,
			})

			// Inject UDS paths for dependencies if co-located
			if isColocPod {
				var allBindings gamev1alpha1.CapabilityBindingList
				if err := r.List(ctx, &allBindings, client.InNamespace(req.Namespace)); err == nil {
					for _, b := range allBindings.Items {
						if b.Spec.Consumer.ModuleManifestName == providerName &&
							b.Spec.WorldRef != nil && b.Spec.WorldRef.Name == world.Name {

							depProvider := b.Spec.Provider.ModuleManifestName
							depGroup := getColocationGroup(&gameDef, depProvider)
							if depGroup != nil && depGroup.Name == colocGroup.Name && depGroup.Strategy == "Pod" {
								envName := fmt.Sprintf("ANVIL_UDS_%s", strings.ToUpper(strings.ReplaceAll(b.Spec.CapabilityID, ".", "_")))
								container.Env = append(container.Env, corev1.EnvVar{
									Name:  envName,
									Value: fmt.Sprintf("/var/run/anvil/%s.sock", depProvider),
								})
							}
						}
					}
				}
			}
		}

		// Service Discovery Injection
		// Find all bindings where this module is the consumer
		var dependencyBindings gamev1alpha1.CapabilityBindingList
		if err := r.List(ctx, &dependencyBindings, client.MatchingFields{idxBindingConsumer: providerName}, client.InNamespace(req.Namespace)); err != nil {
			logger.Error(err, "failed to list dependency bindings for injection")
		} else {
			for _, b := range dependencyBindings.Items {
				// Ensure it's for the same world
				if b.Spec.WorldRef == nil || b.Spec.WorldRef.Name != world.Name {
					continue
				}

				// If endpoint is available, inject it
				if b.Status.Provider != nil && b.Status.Provider.Endpoint != nil {
					ep := b.Status.Provider.Endpoint
					capID := strings.ToUpper(strings.ReplaceAll(b.Spec.CapabilityID, ".", "_"))

					// ANVIL_CAPABILITY_<ID>_ENDPOINT
					container.Env = append(container.Env, corev1.EnvVar{
						Name:  fmt.Sprintf("ANVIL_CAPABILITY_%s_ENDPOINT", capID),
						Value: fmt.Sprintf("%s:%d", ep.Value, ep.Port),
					})

					// ANVIL_CAPABILITY_<ID>_HOST
					container.Env = append(container.Env, corev1.EnvVar{
						Name:  fmt.Sprintf("ANVIL_CAPABILITY_%s_HOST", capID),
						Value: ep.Value,
					})

					// ANVIL_CAPABILITY_<ID>_PORT
					container.Env = append(container.Env, corev1.EnvVar{
						Name:  fmt.Sprintf("ANVIL_CAPABILITY_%s_PORT", capID),
						Value: fmt.Sprintf("%d", ep.Port),
					})
				}
			}
		}

		// Inject dependency endpoints (Service Discovery)
		var myDependencies gamev1alpha1.CapabilityBindingList
		if err := r.List(ctx, &myDependencies, client.InNamespace(req.Namespace), client.MatchingFields{idxBindingConsumer: providerName}); err == nil {
			for _, dep := range myDependencies.Items {
				// Filter by world to ensure we only inject dependencies for THIS world instance
				if dep.Spec.WorldRef == nil || dep.Spec.WorldRef.Name != world.Name {
					continue
				}

				// If the dependency has an endpoint published, inject it.
				if dep.Status.Provider != nil && dep.Status.Provider.Endpoint != nil {
					ep := dep.Status.Provider.Endpoint
					capID := strings.ToUpper(strings.ReplaceAll(dep.Spec.CapabilityID, ".", "_"))

					// ANVIL_CAPABILITY_<ID>_ENDPOINT = host:port
					container.Env = append(container.Env, corev1.EnvVar{
						Name:  fmt.Sprintf("ANVIL_CAPABILITY_%s_ENDPOINT", capID),
						Value: fmt.Sprintf("%s:%d", ep.Value, ep.Port),
					})
					// ANVIL_CAPABILITY_<ID>_HOST = host
					container.Env = append(container.Env, corev1.EnvVar{
						Name:  fmt.Sprintf("ANVIL_CAPABILITY_%s_HOST", capID),
						Value: ep.Value,
					})
					// ANVIL_CAPABILITY_<ID>_PORT = port
					container.Env = append(container.Env, corev1.EnvVar{
						Name:  fmt.Sprintf("ANVIL_CAPABILITY_%s_PORT", capID),
						Value: fmt.Sprintf("%d", ep.Port),
					})
				}
			}
		}

		// Add/Update container in list
		found := false
		for i, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == containerName {
				deployment.Spec.Template.Spec.Containers[i] = container
				found = true
				break
			}
		}
		if !found {
			deployment.Spec.Template.Spec.Containers = append(deployment.Spec.Template.Spec.Containers, container)
		}

		// Handle volumes for this container (non-UDS)
		if volumeToMount != nil {
			// Check if volume already exists in deployment spec
			volFound := false
			for _, v := range deployment.Spec.Template.Spec.Volumes {
				if v.Name == volumeToMount.Name {
					volFound = true
					break
				}
			}
			if !volFound {
				deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, *volumeToMount)
			}
		}

		// Scheduling
		if providerMM.Spec.Scheduling.Affinity != nil {
			deployment.Spec.Template.Spec.Affinity = providerMM.Spec.Scheduling.Affinity
		}
		if len(providerMM.Spec.Scheduling.Tolerations) > 0 {
			deployment.Spec.Template.Spec.Tolerations = providerMM.Spec.Scheduling.Tolerations
		}
		if len(providerMM.Spec.Scheduling.NodeSelector) > 0 {
			deployment.Spec.Template.Spec.NodeSelector = providerMM.Spec.Scheduling.NodeSelector
		}

		// Node Strategy PodAffinity
		if colocGroup != nil && colocGroup.Strategy == "Node" {
			if deployment.Spec.Template.Spec.Affinity == nil {
				deployment.Spec.Template.Spec.Affinity = &corev1.Affinity{}
			}
			if deployment.Spec.Template.Spec.Affinity.PodAffinity == nil {
				deployment.Spec.Template.Spec.Affinity.PodAffinity = &corev1.PodAffinity{}
			}
			term := corev1.PodAffinityTerm{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"game.platform/coloc-group": colocGroup.Name,
						rtLabelWorldName:            world.Name,
					},
				},
				TopologyKey: "kubernetes.io/hostname",
			}
			deployment.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(deployment.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution, term)

			// Ensure labels
			deployment.Labels["game.platform/coloc-group"] = colocGroup.Name
			deployment.Spec.Template.ObjectMeta.Labels["game.platform/coloc-group"] = colocGroup.Name
		}

		if shardObj != nil {
			return controllerutil.SetControllerReference(shardObj, deployment, r.Scheme)
		}
		return controllerutil.SetControllerReference(&world, deployment, r.Scheme)
	})
	if err != nil {
		logger.Error(err, "failed to ensure deployment", "deployment", deploymentName)
		r.recordEventf(&binding, "Warning", "EnsureDeploymentFailed", "Failed to ensure Deployment %q: %v", deploymentName, err)
		anvilControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
		return ctrl.Result{}, err
	}

	// 3) Publish the endpoint back onto the binding status.
	desiredEndpoint := &gamev1alpha1.EndpointRef{
		Type:  "kubernetesService",
		Value: serviceName,
		Port:  port,
	}
	cond := meta.FindStatusCondition(binding.Status.Conditions, BindingConditionRuntimeReady)
	needCondPatch := cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != "EndpointPublished"
	needEndpointPatch := binding.Status.Provider == nil || binding.Status.Provider.Endpoint == nil ||
		binding.Status.Provider.Endpoint.Type != desiredEndpoint.Type ||
		binding.Status.Provider.Endpoint.Value != desiredEndpoint.Value ||
		binding.Status.Provider.Endpoint.Port != desiredEndpoint.Port
	if needEndpointPatch || needCondPatch {
		before := binding.DeepCopy()
		binding.Status.ObservedGeneration = binding.Generation
		binding.Status.Provider = &gamev1alpha1.ProviderStatus{Endpoint: desiredEndpoint}
		setBindingCondition(&binding, metav1.Condition{
			Type:    BindingConditionRuntimeReady,
			Status:  metav1.ConditionTrue,
			Reason:  "EndpointPublished",
			Message: fmt.Sprintf("Endpoint published: %s/%s:%d", desiredEndpoint.Type, desiredEndpoint.Value, desiredEndpoint.Port),
		})
		if err := r.Status().Patch(ctx, &binding, client.MergeFrom(before)); err != nil {
			logger.Error(err, "failed to publish endpoint to binding status", "service", serviceName, "port", port)
			r.recordEventf(&binding, "Warning", "PublishEndpointFailed", "Failed to publish endpoint to binding status: %v", err)
			anvilControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
			return ctrl.Result{}, err
		}
		logger.Info("published endpoint", "endpointType", desiredEndpoint.Type, "endpointValue", desiredEndpoint.Value, "endpointPort", desiredEndpoint.Port)
		r.recordEventf(&binding, "Normal", "EndpointPublished", "Published endpoint %s/%s:%d", desiredEndpoint.Type, desiredEndpoint.Value, desiredEndpoint.Port)
	}

	// Update the owning world's RuntimeReady condition (best-effort for debuggability).
	if err := r.updateWorldRuntimeReadyCondition(ctx, req.Namespace, &world); err != nil {
		logger.Error(err, "failed to update world RuntimeReady condition")
	}

	logger.Info("ensured runtime", "service", serviceName, "image", image, "port", port)
	return ctrl.Result{}, nil
}

func (r *RuntimeOrchestratorReconciler) updateWorldRuntimeReadyCondition(ctx context.Context, namespace string, world *gamev1alpha1.WorldInstance) error {
	if world == nil {
		return nil
	}

	var bindings gamev1alpha1.CapabilityBindingList
	if err := r.List(ctx, &bindings,
		client.InNamespace(namespace),
		client.MatchingLabels{labelManagedBy: managedByCapabilityResolver, labelWorldName: world.Name},
	); err != nil {
		return err
	}

	total := 0
	ready := 0
	missingProviders := 0
	for i := range bindings.Items {
		b := &bindings.Items[i]
		provider := strings.TrimSpace(b.Spec.Provider.ModuleManifestName)
		if provider == "" {
			continue
		}
		var mm gamev1alpha1.ModuleManifest
		if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: provider}, &mm); err != nil {
			if apierrors.IsNotFound(err) {
				missingProviders++
				total++
				continue
			}
			return err
		}
		if strings.TrimSpace(mm.Annotations[annRuntimeImage]) == "" {
			// Not a server workload.
			continue
		}
		total++
		if b.Status.Provider != nil && b.Status.Provider.Endpoint != nil {
			ready++
		}
	}

	status := metav1.ConditionTrue
	reason := "NoServerWorkloads"
	message := runtimeReadyMessage(ready, total)
	if total > 0 {
		if missingProviders > 0 {
			status = metav1.ConditionFalse
			reason = "ProviderNotFound"
			message = fmt.Sprintf("%d provider ModuleManifest(s) missing", missingProviders)
		} else if ready < total {
			status = metav1.ConditionFalse
			reason = "WaitingForEndpoints"
		}
		if status == metav1.ConditionTrue {
			reason = "EndpointsPublished"
		}
	}

	before := world.DeepCopy()
	prev := meta.FindStatusCondition(world.Status.Conditions, WorldConditionRuntimeReady)
	setWorldCondition(world, metav1.Condition{Type: WorldConditionRuntimeReady, Status: status, Reason: reason, Message: message})
	world.Status.ObservedGeneration = world.Generation
	if err := r.Status().Patch(ctx, world, client.MergeFrom(before)); err != nil {
		return err
	}
	if (prev == nil || prev.Status != metav1.ConditionTrue) && status == metav1.ConditionTrue {
		r.recordEventf(world, "Normal", "RuntimeReady", "%s", message)
	}
	return nil
}

func (r *RuntimeOrchestratorReconciler) recordEventf(obj client.Object, eventType, reason, messageFmt string, args ...any) {
	if r.Recorder == nil || obj == nil {
		return
	}
	r.Recorder.Eventf(obj, eventType, reason, messageFmt, args...)
}

func (r *RuntimeOrchestratorReconciler) findConsumersForBinding(ctx context.Context, obj client.Object) []reconcile.Request {
	binding, ok := obj.(*gamev1alpha1.CapabilityBinding)
	if !ok {
		return nil
	}

	// If this binding has a consumer, we want to find the binding that MANAGES that consumer (as a provider).
	// Because that managing binding is responsible for the Deployment of the consumer.
	consumerModule := binding.Spec.Consumer.ModuleManifestName
	if consumerModule == "" {
		return nil
	}

	// Find bindings where Provider == consumerModule
	var bindings gamev1alpha1.CapabilityBindingList
	if err := r.List(ctx, &bindings, client.MatchingFields{idxBindingProvider: consumerModule}, client.InNamespace(binding.Namespace)); err != nil {
		return nil
	}

	var reqs []reconcile.Request
	for _, b := range bindings.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: b.Name, Namespace: b.Namespace}})
	}
	return reqs
}

func (r *RuntimeOrchestratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Index spec.consumer.moduleManifestName
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gamev1alpha1.CapabilityBinding{}, idxBindingConsumer, func(rawObj client.Object) []string {
		binding := rawObj.(*gamev1alpha1.CapabilityBinding)
		if binding.Spec.Consumer.ModuleManifestName == "" {
			return nil
		}
		return []string{binding.Spec.Consumer.ModuleManifestName}
	}); err != nil {
		return err
	}

	// Index spec.provider.moduleManifestName
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gamev1alpha1.CapabilityBinding{}, idxBindingProvider, func(rawObj client.Object) []string {
		binding := rawObj.(*gamev1alpha1.CapabilityBinding)
		if binding.Spec.Provider.ModuleManifestName == "" {
			return nil
		}
		return []string{binding.Spec.Provider.ModuleManifestName}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&gamev1alpha1.CapabilityBinding{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Watches(
			&gamev1alpha1.CapabilityBinding{},
			handler.EnqueueRequestsFromMapFunc(r.findConsumersForBinding),
		).
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

func rtNameWithShard(worldName, shardID, moduleName string) string {
	if strings.TrimSpace(shardID) == "" {
		return rtName(worldName, moduleName)
	}
	base := fmt.Sprintf("rt-%s-shard-%s-%s", worldName, shardID, moduleName)
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

func parseCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stableWSCName(worldName, shardName, tier string) string {
	base := fmt.Sprintf("wsc-%s-%s-%s", worldName, shardName, tier)
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

func (r *RuntimeOrchestratorReconciler) ensureWorldStorageClaim(
	ctx context.Context,
	namespace string,
	world *gamev1alpha1.WorldInstance,
	shard *gamev1alpha1.WorldShard,
	name string,
	scope gamev1alpha1.WorldStorageScope,
	tier gamev1alpha1.WorldStorageTier,
	size string,
	accessModes []string,
	shardIDLabel string,
	shardObjectName string,
) error {
	claim := &gamev1alpha1.WorldStorageClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, claim, func() error {
		if claim.Labels == nil {
			claim.Labels = map[string]string{}
		}
		claim.Labels[rtLabelManagedBy] = rtManagedBy
		claim.Labels[labelWorldName] = world.Name
		if shardIDLabel != "" {
			claim.Labels[labelShardID] = shardIDLabel
		}
		claim.Spec.Scope = scope
		claim.Spec.Tier = tier
		claim.Spec.WorldRef = gamev1alpha1.ObjectRef{Name: world.Name}
		if scope == gamev1alpha1.WorldStorageScopeWorldShard {
			claim.Spec.ShardRef = &gamev1alpha1.ObjectRef{Name: shardObjectName}
		} else {
			claim.Spec.ShardRef = nil
		}
		claim.Spec.Size = size
		claim.Spec.AccessModes = accessModes
		if shard != nil {
			return controllerutil.SetControllerReference(shard, claim, r.Scheme)
		}
		return controllerutil.SetControllerReference(world, claim, r.Scheme)
	})
	return err
}

func getColocationGroup(game *gamev1alpha1.GameDefinition, moduleName string) *gamev1alpha1.ColocationGroup {
	if game == nil {
		return nil
	}
	for _, group := range game.Spec.Colocation {
		for _, m := range group.Modules {
			if m == moduleName {
				return &group
			}
		}
	}
	return nil
}
