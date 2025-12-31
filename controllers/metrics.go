package controllers

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	binderyControllerReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bindery_controller_reconcile_total",
			Help: "Number of reconciliations by controller.",
		},
		[]string{"controller"},
	)
	binderyControllerReconcileErrorTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bindery_controller_reconcile_error_total",
			Help: "Number of reconciliation errors by controller.",
		},
		[]string{"controller"},
	)

	capabilityResolverUnresolvedRequired = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "bindery_capabilityresolver_unresolved_required",
			Help: "Number of unresolved required requirements observed in the last CapabilityResolver reconcile.",
		},
	)

	capabilityResolverBindingsCreatedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bindery_capabilityresolver_bindings_created_total",
			Help: "Total number of CapabilityBindings created by CapabilityResolver.",
		},
	)
	capabilityResolverBindingsUpdatedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bindery_capabilityresolver_bindings_updated_total",
			Help: "Total number of CapabilityBindings updated by CapabilityResolver.",
		},
	)
	capabilityResolverBindingsDeletedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "bindery_capabilityresolver_bindings_deleted_total",
			Help: "Total number of CapabilityBindings deleted by CapabilityResolver.",
		},
	)

	capabilityResolverResolutionDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "bindery_capabilityresolver_resolution_duration_seconds",
			Help:    "Time taken to resolve capabilities.",
			Buckets: prometheus.DefBuckets,
		},
	)

	runtimeOrchestratorDeploymentDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "bindery_runtimeorchestrator_deployment_duration_seconds",
			Help:    "Time taken to ensure deployment.",
			Buckets: prometheus.DefBuckets,
		},
	)
)

func init() {
	metrics.Registry.MustRegister(
		binderyControllerReconcileTotal,
		binderyControllerReconcileErrorTotal,
		capabilityResolverUnresolvedRequired,
		capabilityResolverBindingsCreatedTotal,
		capabilityResolverBindingsUpdatedTotal,
		capabilityResolverBindingsDeletedTotal,
		capabilityResolverResolutionDuration,
		runtimeOrchestratorDeploymentDuration,
	)
}
