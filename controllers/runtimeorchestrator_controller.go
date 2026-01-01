package controllers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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

	binderyv1alpha1 "github.com/bayleafwalker/bindery-core/api/v1alpha1"
)

const (
	rtLabelManagedBy = "bindery.platform/managed-by"
	rtLabelWorldName = "bindery.platform/world"
	rtLabelModule    = "bindery.platform/module"

	rtManagedBy = "runtimeorchestrator"

	idxBindingConsumer = "spec.consumer.moduleManifestName"
	idxBindingProvider = "spec.provider.moduleManifestName"

	annRuntimeImage = "bindery.dev/runtime-image"
	annRuntimePort  = "bindery.dev/runtime-port"

	annStorageTier        = "bindery.dev/storage-tier"
	annStorageSize        = "bindery.dev/storage-size"
	annStorageScope       = "bindery.dev/storage-scope"
	annStorageAccessModes = "bindery.dev/storage-access-modes"
	annStorageMountPath   = "bindery.dev/storage-mount-path"

	annTerminationGracePeriod = "bindery.dev/termination-grace-period"
	annPreStopCommand         = "bindery.dev/pre-stop-command"
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
// +kubebuilder:rbac:groups=bindery.platform,resources=capabilitybindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=bindery.platform,resources=capabilitybindings/status,verbs=update;patch
// +kubebuilder:rbac:groups=bindery.platform,resources=worldinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=bindery.platform,resources=worldshards,verbs=get;list;watch
// +kubebuilder:rbac:groups=bindery.platform,resources=modulemanifests,verbs=get;list;watch
// +kubebuilder:rbac:groups=bindery.platform,resources=worldstorageclaims,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=bindery.platform,resources=worldstorageclaims/status,verbs=get
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
type RuntimeOrchestratorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	// Name allows overriding the controller name (useful for tests to avoid global collisions).
	Name string
}

func (r *RuntimeOrchestratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	binderyControllerReconcileTotal.WithLabelValues("RuntimeOrchestrator").Inc()

	logger := log.FromContext(ctx).WithValues(
		"controller", "RuntimeOrchestrator",
		"namespace", req.Namespace,
		"binding", req.Name,
	)

	var binding binderyv1alpha1.CapabilityBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		binderyControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
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

	// Only orchestrate bindings that are scoped to a world, unless they are global.
	isGlobal := binding.Spec.Scope == binderyv1alpha1.CapabilityScopeCluster || binding.Spec.Scope == binderyv1alpha1.CapabilityScopeRegion || binding.Spec.Scope == binderyv1alpha1.CapabilityScopeRealm
	if !isGlobal && (binding.Spec.WorldRef == nil || binding.Spec.WorldRef.Name == "") {
		logger.V(1).Info("skipping binding without world")
		return ctrl.Result{}, nil
	}
	if !isGlobal {
		logger = logger.WithValues("world", binding.Spec.WorldRef.Name)
	}

	// Load owning world (needed for ownerRefs/labels).
	var world binderyv1alpha1.WorldInstance
	if !isGlobal {
		if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: binding.Spec.WorldRef.Name}, &world); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("world not found; skipping")
				return ctrl.Result{}, nil
			}
			logger.Error(err, "failed to load world")
			r.recordEventf(&binding, "Warning", "GetWorldFailed", "Failed to get WorldInstance %q: %v", binding.Spec.WorldRef.Name, err)
			binderyControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
			return ctrl.Result{}, err
		}
	}

	// Load Booklet for colocation logic
	var booklet binderyv1alpha1.Booklet
	if !isGlobal {
		if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: world.Spec.GameRef.Name}, &booklet); err != nil {
			logger.V(1).Info("game definition not found; proceeding without colocation logic", "game", world.Spec.GameRef.Name)
		}
	}

	var shardObj *binderyv1alpha1.WorldShard
	shardName := ""
	if !isGlobal && shardLabel != "" {
		id, err := strconv.Atoi(shardLabel)
		if err != nil || id < 0 {
			logger.V(1).Info("invalid shard label; treating as non-sharded binding", "labelValue", shardLabel)
			shardLabel = ""
		} else {
			shardName = stableWorldShardName(world.Name, int32(id))
			var ws binderyv1alpha1.WorldShard
			if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: shardName}, &ws); err != nil {
				if apierrors.IsNotFound(err) {
					logger.V(1).Info("worldshard not found yet; requeue", "worldShard", shardName)
					return ctrl.Result{Requeue: true}, nil
				}
				logger.Error(err, "failed to get worldshard", "worldShard", shardName)
				binderyControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
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

	var providerMM binderyv1alpha1.ModuleManifest
	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: providerName}, &providerMM); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("provider modulemanifest not found; skipping")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to load provider modulemanifest")
		r.recordEventf(&binding, "Warning", "GetProviderModuleFailed", "Failed to get provider ModuleManifest %q: %v", providerName, err)
		binderyControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
		return ctrl.Result{}, err
	}

	runtimeSpec := providerMM.Spec.Runtime

	image := ""
	if runtimeSpec != nil {
		image = strings.TrimSpace(runtimeSpec.Image)
	}
	if image == "" {
		image = strings.TrimSpace(providerMM.Annotations[annRuntimeImage])
	}
	if image == "" {
		// Convention: no runtime config means "not server-orchestrated".
		logger.V(1).Info("provider not server-orchestrated (missing runtime image)")
		// Mark binding as runtime-ready (not applicable) for debuggability.
		before := binding.DeepCopy()
		binding.Status.ObservedGeneration = binding.Generation
		setBindingCondition(&binding, metav1.Condition{
			Type:    BindingConditionRuntimeReady,
			Status:  metav1.ConditionTrue,
			Reason:  "NotServerOrchestrated",
			Message: "Provider has no runtime config; no server workload will be created",
		})
		_ = r.Status().Patch(ctx, &binding, client.MergeFrom(before))
		return ctrl.Result{}, nil
	}

	port := int32(50051)
	if runtimeSpec != nil && runtimeSpec.Port != nil && *runtimeSpec.Port > 0 && *runtimeSpec.Port <= 65535 {
		port = *runtimeSpec.Port
	} else if raw := strings.TrimSpace(providerMM.Annotations[annRuntimePort]); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 && p <= 65535 {
			port = int32(p)
		} else {
			logger.V(1).Info("invalid runtime port annotation; using default", "annotationValue", raw, "defaultPort", port)
		}
	}

	// Determine colocation
	var colocGroup *binderyv1alpha1.ColocationGroup
	if !isGlobal {
		colocGroup = getColocationGroup(&booklet, providerName)
	}
	isColocPod := colocGroup != nil && colocGroup.Strategy == "Pod"

	// Stable names shared by Service and Deployment.
	// For Service, we always use the module-specific name.
	worldName := "global"
	if !isGlobal {
		worldName = world.Name
	}
	serviceName := rtNameWithShard(worldName, shardLabel, providerName)

	// For Deployment, we might use a shared name.
	deploymentName := rtNameWithShard(worldName, shardLabel, providerName)
	if isColocPod {
		deploymentName = rtNameWithShard(worldName, shardLabel, "coloc-"+colocGroup.Name)
	}

	labels := map[string]string{
		rtLabelManagedBy: rtManagedBy,
	}
	if !isGlobal {
		labels[rtLabelWorldName] = world.Name
	}
	if shardLabel != "" {
		labels[labelShardID] = shardLabel
	}

	// Service labels
	serviceLabels := mergeLabels(nil, labels)
	serviceLabels[rtLabelModule] = providerName
	if isColocPod {
		serviceLabels["bindery.platform/coloc-group"] = colocGroup.Name
	}

	// Deployment labels
	deploymentLabels := mergeLabels(nil, labels)
	if isColocPod {
		deploymentLabels["bindery.platform/coloc-group"] = colocGroup.Name
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
		storageMountPath = "/var/bindery/state"
	}
	storageScopeRaw := strings.TrimSpace(providerMM.Annotations[annStorageScope])
	if storageScopeRaw == "" {
		if shardLabel != "" {
			storageScopeRaw = string(binderyv1alpha1.WorldStorageScopeWorldShard)
		} else {
			storageScopeRaw = string(binderyv1alpha1.WorldStorageScopeWorld)
		}
	}
	accessModes := parseCSV(providerMM.Annotations[annStorageAccessModes])

	var volumeToMount *corev1.Volume
	var mountToUse *corev1.VolumeMount
	if !isGlobal && storageTierRaw != "" {
		tier := binderyv1alpha1.WorldStorageTier(storageTierRaw)
		scope := binderyv1alpha1.WorldStorageScope(storageScopeRaw)
		if tier == binderyv1alpha1.WorldStorageTierServerLowLatency || tier == binderyv1alpha1.WorldStorageTierServerHighLatency {
			// Ensure claim exists; StorageOrchestrator will materialize the PVC.
			claimName := stableWSCName(world.Name, shardName, storageTierRaw)
			if err := r.ensureWorldStorageClaim(ctx, req.Namespace, &world, shardObj, claimName, scope, tier, storageSize, accessModes, shardLabel, shardName); err != nil {
				logger.Error(err, "failed to ensure world storage claim", "claim", claimName)
				r.recordEventf(&binding, "Warning", "EnsureWorldStorageClaimFailed", "Failed to ensure WorldStorageClaim %q: %v", claimName, err)
				return ctrl.Result{}, err
			}
			pvcName := stablePVCName(world.Name, shardName, storageTierRaw)
			volumeToMount = &corev1.Volume{
				Name: "bindery-state",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName},
				},
			}
			mountToUse = &corev1.VolumeMount{Name: "bindery-state", MountPath: storageMountPath}
		} else {
			logger.V(1).Info("storage tier not supported for server workload; skipping", "tier", storageTierRaw)
		}
	}

	// 1) Ensure Service
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: req.Namespace}}
	serviceOwner := controllerOwner(shardObj, &world, &binding, isGlobal)
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		existingOwner := metav1.GetControllerOf(service)
		if existingOwner != nil && (serviceOwner == nil || !metav1.IsControlledBy(service, serviceOwner)) {
			logger.V(1).Info("service already owned by another controller; reusing", "service", serviceName, "owner", fmt.Sprintf("%s/%s", existingOwner.Kind, existingOwner.Name))
			return nil
		}

		service.Labels = mergeLabels(service.Labels, serviceLabels)

		// Selector logic
		selector := map[string]string{
			rtLabelManagedBy: rtManagedBy,
		}
		if !isGlobal {
			selector[rtLabelWorldName] = world.Name
		}
		if shardLabel != "" {
			selector[labelShardID] = shardLabel
		}
		if isColocPod {
			selector["bindery.platform/coloc-group"] = colocGroup.Name
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
		if serviceOwner != nil {
			return controllerutil.SetControllerReference(serviceOwner, service, r.Scheme)
		}
		return nil
	})
	if err != nil {
		logger.Error(err, "failed to ensure service", "service", serviceName)
		r.recordEventf(&binding, "Warning", "EnsureServiceFailed", "Failed to ensure Service %q: %v", serviceName, err)
		binderyControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
		return ctrl.Result{}, err
	}

	// 2) Ensure Deployment
	startDep := time.Now()
	// Graceful termination settings
	var terminationGracePeriod *int64
	if runtimeSpec != nil && runtimeSpec.TerminationGracePeriodSeconds != nil {
		terminationGracePeriod = runtimeSpec.TerminationGracePeriodSeconds
	} else if val := strings.TrimSpace(providerMM.Annotations[annTerminationGracePeriod]); val != "" {
		if sec, err := strconv.ParseInt(val, 10, 64); err == nil && sec >= 0 {
			terminationGracePeriod = &sec
		}
	}
	preStopCommand := ""
	if runtimeSpec != nil {
		preStopCommand = strings.TrimSpace(runtimeSpec.PreStopCommand)
	}
	if preStopCommand == "" {
		preStopCommand = strings.TrimSpace(providerMM.Annotations[annPreStopCommand])
	}

	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: req.Namespace}}
	deploymentOwner := controllerOwner(shardObj, &world, &binding, isGlobal)
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		existingOwner := metav1.GetControllerOf(deployment)
		if existingOwner != nil && (deploymentOwner == nil || !metav1.IsControlledBy(deployment, deploymentOwner)) {
			logger.V(1).Info("deployment already owned by another controller; reusing", "deployment", deploymentName, "owner", fmt.Sprintf("%s/%s", existingOwner.Kind, existingOwner.Name))
			return nil
		}

		deployment.Labels = mergeLabels(deployment.Labels, deploymentLabels)
		deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: deploymentLabels}
		deployment.Spec.Replicas = int32Ptr(1)
		deployment.Spec.Template.ObjectMeta.Labels = mergeLabels(deployment.Spec.Template.ObjectMeta.Labels, deploymentLabels)

		if terminationGracePeriod != nil {
			deployment.Spec.Template.Spec.TerminationGracePeriodSeconds = terminationGracePeriod
		}

		// Container logic
		containerName := "module"
		if isColocPod {
			containerName = providerName
		}

		container := corev1.Container{
			Name:  containerName,
			Image: image,
			Ports: []corev1.ContainerPort{{ContainerPort: port, Name: "grpc"}},
		}
		if runtimeSpec != nil {
			if len(runtimeSpec.Command) > 0 {
				container.Command = append([]string(nil), runtimeSpec.Command...)
			}
			if len(runtimeSpec.Args) > 0 {
				container.Args = append([]string(nil), runtimeSpec.Args...)
			}
		}
		if volumeToMount != nil && mountToUse != nil {
			container.VolumeMounts = append(container.VolumeMounts, *mountToUse)
		}

		if preStopCommand != "" {
			container.Lifecycle = &corev1.Lifecycle{
				PreStop: &corev1.LifecycleHandler{
					Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", preStopCommand}},
				},
			}
		}

		// Environment (base + platform-injected).
		env := map[string]string{}
		if runtimeSpec != nil && len(runtimeSpec.Env) > 0 {
			for k, v := range runtimeSpec.Env {
				env[k] = v
			}
		}

		// List dependencies once for service discovery + UDS hints.
		var deps []binderyv1alpha1.CapabilityBinding
		{
			var depList binderyv1alpha1.CapabilityBindingList
			if err := r.List(ctx, &depList,
				client.InNamespace(req.Namespace),
				client.MatchingFields{idxBindingConsumer: providerName},
			); err != nil {
				logger.Error(err, "failed to list dependency bindings for injection")
			} else {
				for i := range depList.Items {
					dep := depList.Items[i]

					// World filtering: world-scoped deployments should only see their own bindings.
					if !isGlobal {
						if dep.Spec.WorldRef == nil || dep.Spec.WorldRef.Name != world.Name {
							continue
						}
					}

					// Shard filtering: never inject shard-scoped deps into non-sharded deployments.
					if dep.Spec.Scope == binderyv1alpha1.CapabilityScopeWorldShard {
						depShard := ""
						if dep.Labels != nil {
							depShard = strings.TrimSpace(dep.Labels[labelShardID])
						}
						if strings.TrimSpace(shardLabel) == "" {
							continue
						}
						if depShard != shardLabel {
							continue
						}
					}

					deps = append(deps, dep)
				}
			}
		}

		sort.Slice(deps, func(i, j int) bool {
			a := deps[i].Spec
			b := deps[j].Spec
			if a.CapabilityID != b.CapabilityID {
				return a.CapabilityID < b.CapabilityID
			}
			if a.Scope != b.Scope {
				return a.Scope < b.Scope
			}
			if a.Provider.ModuleManifestName != b.Provider.ModuleManifestName {
				return a.Provider.ModuleManifestName < b.Provider.ModuleManifestName
			}
			return deps[i].Name < deps[j].Name
		})

		// UDS (Pod co-location) support.
		if isColocPod {
			// Add shared volume if not present.
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

			// Mount volume in container.
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      "shared-socket",
				MountPath: "/var/run/bindery",
			})

			env["BINDERY_UDS_DIR"] = "/var/run/bindery"
			env["BINDERY_MODULE_NAME"] = providerName

			// Inject UDS socket paths for dependencies that are in the same Pod co-location group.
			for _, dep := range deps {
				depProvider := strings.TrimSpace(dep.Spec.Provider.ModuleManifestName)
				if depProvider == "" {
					continue
				}
				depGroup := getColocationGroup(&booklet, depProvider)
				if depGroup == nil || colocGroup == nil || depGroup.Name != colocGroup.Name || depGroup.Strategy != "Pod" {
					continue
				}
				envName := fmt.Sprintf("BINDERY_UDS_%s", strings.ToUpper(strings.ReplaceAll(dep.Spec.CapabilityID, ".", "_")))
				env[envName] = fmt.Sprintf("/var/run/bindery/%s.sock", depProvider)
			}
		}

		// Service discovery injection: publish resolved endpoints to env vars.
		for _, dep := range deps {
			if dep.Status.Provider == nil || dep.Status.Provider.Endpoint == nil {
				continue
			}
			ep := dep.Status.Provider.Endpoint
			capID := strings.ToUpper(strings.ReplaceAll(dep.Spec.CapabilityID, ".", "_"))
			env[fmt.Sprintf("BINDERY_CAPABILITY_%s_ENDPOINT", capID)] = fmt.Sprintf("%s:%d", ep.Value, ep.Port)
			env[fmt.Sprintf("BINDERY_CAPABILITY_%s_HOST", capID)] = ep.Value
			env[fmt.Sprintf("BINDERY_CAPABILITY_%s_PORT", capID)] = fmt.Sprintf("%d", ep.Port)
		}

		// Readiness coordination via init container: only for non-pod-colocated deployments.
		waitTargets := make(map[string]struct{})
		if !isColocPod {
			providerCache := make(map[string]*binderyv1alpha1.ModuleManifest)
			for _, dep := range deps {
				depProvider := strings.TrimSpace(dep.Spec.Provider.ModuleManifestName)
				if depProvider == "" {
					continue
				}

				depMM, ok := providerCache[depProvider]
				if !ok {
					var mm binderyv1alpha1.ModuleManifest
					if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: depProvider}, &mm); err != nil {
						continue
					}
					depMM = &mm
					providerCache[depProvider] = depMM
				}
				if !isServerOrchestrated(depMM) {
					continue
				}

				targetService := serviceNameForBinding(dep)
				if targetService == "" {
					continue
				}
				targetPort := runtimePortForModule(depMM)
				waitTargets[fmt.Sprintf("%s:%d", targetService, targetPort)] = struct{}{}
			}
		}

		// Add/update/remove the init container to avoid stale waits.
		const waitInitName = "wait-for-deps"
		if len(waitTargets) > 0 && !isColocPod {
			targets := make([]string, 0, len(waitTargets))
			for t := range waitTargets {
				targets = append(targets, t)
			}
			sort.Strings(targets)

			cmd := "for target in " + strings.Join(targets, " ") + "; do " +
				"host=${target%%:*}; port=${target##*:}; " +
				"until nc -z -w 2 $host $port; do echo waiting for $target; sleep 2; done; " +
				"done"
			initContainer := corev1.Container{
				Name:    waitInitName,
				Image:   "busybox:1.36",
				Command: []string{"sh", "-c", cmd},
			}

			foundInit := false
			for i := range deployment.Spec.Template.Spec.InitContainers {
				if deployment.Spec.Template.Spec.InitContainers[i].Name == waitInitName {
					deployment.Spec.Template.Spec.InitContainers[i] = initContainer
					foundInit = true
					break
				}
			}
			if !foundInit {
				deployment.Spec.Template.Spec.InitContainers = append(deployment.Spec.Template.Spec.InitContainers, initContainer)
			}
		} else {
			// Remove wait init container if present.
			next := deployment.Spec.Template.Spec.InitContainers[:0]
			for _, c := range deployment.Spec.Template.Spec.InitContainers {
				if c.Name == waitInitName {
					continue
				}
				next = append(next, c)
			}
			deployment.Spec.Template.Spec.InitContainers = next
		}

		container.Env = envVarsFromMap(env)

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
		if providerMM.Spec.Scheduling.PriorityClassName != "" {
			deployment.Spec.Template.Spec.PriorityClassName = providerMM.Spec.Scheduling.PriorityClassName
		}

		// Node Strategy PodAffinity
		if colocGroup != nil && colocGroup.Strategy == "Node" {
			if deployment.Spec.Template.Spec.Affinity == nil {
				deployment.Spec.Template.Spec.Affinity = &corev1.Affinity{}
			}
			if deployment.Spec.Template.Spec.Affinity.PodAffinity == nil {
				deployment.Spec.Template.Spec.Affinity.PodAffinity = &corev1.PodAffinity{}
			}

			matchLabels := map[string]string{
				"bindery.platform/coloc-group": colocGroup.Name,
				rtLabelWorldName:               world.Name,
			}
			if shardLabel != "" {
				matchLabels[labelShardID] = shardLabel
			}

			term := corev1.PodAffinityTerm{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: matchLabels,
				},
				TopologyKey: "kubernetes.io/hostname",
			}
			deployment.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(deployment.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution, term)

			// Ensure labels
			deployment.Labels["bindery.platform/coloc-group"] = colocGroup.Name
			deployment.Spec.Template.ObjectMeta.Labels["bindery.platform/coloc-group"] = colocGroup.Name
		}

		if deploymentOwner != nil {
			return controllerutil.SetControllerReference(deploymentOwner, deployment, r.Scheme)
		}
		return nil
	})
	runtimeOrchestratorDeploymentDuration.Observe(time.Since(startDep).Seconds())
	if err != nil {
		logger.Error(err, "failed to ensure deployment", "deployment", deploymentName)
		r.recordEventf(&binding, "Warning", "EnsureDeploymentFailed", "Failed to ensure Deployment %q: %v", deploymentName, err)
		binderyControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
		return ctrl.Result{}, err
	}

	// 3) Publish the endpoint back onto the binding status.
	desiredEndpoint := &binderyv1alpha1.EndpointRef{
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
		binding.Status.Provider = &binderyv1alpha1.ProviderStatus{Endpoint: desiredEndpoint}
		setBindingCondition(&binding, metav1.Condition{
			Type:    BindingConditionRuntimeReady,
			Status:  metav1.ConditionTrue,
			Reason:  "EndpointPublished",
			Message: fmt.Sprintf("Endpoint published: %s/%s:%d", desiredEndpoint.Type, desiredEndpoint.Value, desiredEndpoint.Port),
		})
		if err := r.Status().Patch(ctx, &binding, client.MergeFrom(before)); err != nil {
			logger.Error(err, "failed to publish endpoint to binding status", "service", serviceName, "port", port)
			r.recordEventf(&binding, "Warning", "PublishEndpointFailed", "Failed to publish endpoint to binding status: %v", err)
			binderyControllerReconcileErrorTotal.WithLabelValues("RuntimeOrchestrator").Inc()
			return ctrl.Result{}, err
		}
		logger.Info("published endpoint", "endpointType", desiredEndpoint.Type, "endpointValue", desiredEndpoint.Value, "endpointPort", desiredEndpoint.Port)
		r.recordEventf(&binding, "Normal", "EndpointPublished", "Published endpoint %s/%s:%d", desiredEndpoint.Type, desiredEndpoint.Value, desiredEndpoint.Port)
	}

	// Update the owning world's RuntimeReady condition (best-effort for debuggability).
	if !isGlobal {
		if err := r.updateWorldRuntimeReadyCondition(ctx, req.Namespace, &world); err != nil {
			logger.Error(err, "failed to update world RuntimeReady condition")
			// Return error to retry, as this condition is critical for e2e tests
			return ctrl.Result{}, err
		}
	}

	logger.Info("ensured runtime", "service", serviceName, "image", image, "port", port)
	return ctrl.Result{}, nil
}

func (r *RuntimeOrchestratorReconciler) updateWorldRuntimeReadyCondition(ctx context.Context, namespace string, world *binderyv1alpha1.WorldInstance) error {
	if world == nil || strings.TrimSpace(world.Name) == "" {
		return nil
	}

	// Re-fetch world to avoid overwriting other conditions due to stale cache (JSON merge patch replaces list)
	latestWorld := &binderyv1alpha1.WorldInstance{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: world.Name}, latestWorld); err != nil {
		return err
	}
	world = latestWorld

	var bindings binderyv1alpha1.CapabilityBindingList
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
		var mm binderyv1alpha1.ModuleManifest
		if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: provider}, &mm); err != nil {
			if apierrors.IsNotFound(err) {
				missingProviders++
				total++
				continue
			}
			return err
		}
		if !isServerOrchestrated(&mm) {
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
	binding, ok := obj.(*binderyv1alpha1.CapabilityBinding)
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
	var bindings binderyv1alpha1.CapabilityBindingList
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
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &binderyv1alpha1.CapabilityBinding{}, idxBindingConsumer, func(rawObj client.Object) []string {
		binding := rawObj.(*binderyv1alpha1.CapabilityBinding)
		if binding.Spec.Consumer.ModuleManifestName == "" {
			return nil
		}
		return []string{binding.Spec.Consumer.ModuleManifestName}
	}); err != nil {
		return err
	}

	// Index spec.provider.moduleManifestName
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &binderyv1alpha1.CapabilityBinding{}, idxBindingProvider, func(rawObj client.Object) []string {
		binding := rawObj.(*binderyv1alpha1.CapabilityBinding)
		if binding.Spec.Provider.ModuleManifestName == "" {
			return nil
		}
		return []string{binding.Spec.Provider.ModuleManifestName}
	}); err != nil {
		return err
	}

	builder := ctrl.NewControllerManagedBy(mgr)
	if r.Name != "" {
		builder = builder.Named(r.Name)
	}

	return builder.
		For(&binderyv1alpha1.CapabilityBinding{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Watches(
			&binderyv1alpha1.CapabilityBinding{},
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

func controllerOwner(shard *binderyv1alpha1.WorldShard, world *binderyv1alpha1.WorldInstance, binding *binderyv1alpha1.CapabilityBinding, isGlobal bool) client.Object {
	if shard != nil {
		return shard
	}
	if !isGlobal {
		return world
	}
	return binding
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
	world *binderyv1alpha1.WorldInstance,
	shard *binderyv1alpha1.WorldShard,
	name string,
	scope binderyv1alpha1.WorldStorageScope,
	tier binderyv1alpha1.WorldStorageTier,
	size string,
	accessModes []string,
	shardIDLabel string,
	shardObjectName string,
) error {
	claim := &binderyv1alpha1.WorldStorageClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
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
		claim.Spec.WorldRef = binderyv1alpha1.ObjectRef{Name: world.Name}
		if scope == binderyv1alpha1.WorldStorageScopeWorldShard {
			claim.Spec.ShardRef = &binderyv1alpha1.ObjectRef{Name: shardObjectName}
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

func getColocationGroup(game *binderyv1alpha1.Booklet, moduleName string) *binderyv1alpha1.ColocationGroup {
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

func envVarsFromMap(env map[string]string) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]corev1.EnvVar, 0, len(keys))
	for _, k := range keys {
		out = append(out, corev1.EnvVar{Name: k, Value: env[k]})
	}
	return out
}

func isServerOrchestrated(mm *binderyv1alpha1.ModuleManifest) bool {
	if mm == nil {
		return false
	}
	if mm.Spec.Runtime != nil && strings.TrimSpace(mm.Spec.Runtime.Image) != "" {
		return true
	}
	return strings.TrimSpace(mm.Annotations[annRuntimeImage]) != ""
}

func runtimePortForModule(mm *binderyv1alpha1.ModuleManifest) int32 {
	if mm == nil {
		return 50051
	}
	if mm.Spec.Runtime != nil && mm.Spec.Runtime.Port != nil && *mm.Spec.Runtime.Port > 0 && *mm.Spec.Runtime.Port <= 65535 {
		return *mm.Spec.Runtime.Port
	}
	if raw := strings.TrimSpace(mm.Annotations[annRuntimePort]); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 && p <= 65535 {
			return int32(p)
		}
	}
	return 50051
}

func serviceNameForBinding(binding binderyv1alpha1.CapabilityBinding) string {
	provider := strings.TrimSpace(binding.Spec.Provider.ModuleManifestName)
	if provider == "" {
		return ""
	}

	worldName := "global"
	switch binding.Spec.Scope {
	case binderyv1alpha1.CapabilityScopeCluster, binderyv1alpha1.CapabilityScopeRegion, binderyv1alpha1.CapabilityScopeRealm:
		// Global service per cluster/region/realm scope (MVP uses a single shared name).
	default:
		if binding.Spec.WorldRef != nil && binding.Spec.WorldRef.Name != "" {
			worldName = binding.Spec.WorldRef.Name
		}
	}

	shard := ""
	if binding.Spec.Scope == binderyv1alpha1.CapabilityScopeWorldShard && binding.Labels != nil {
		shard = strings.TrimSpace(binding.Labels[labelShardID])
	}

	return rtNameWithShard(worldName, shard, provider)
}
