package controllers

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
)

const (
	realmManagedBy = "realm-controller"
)

// RealmReconciler reconciles a Realm object
//
// RBAC:
// +kubebuilder:rbac:groups=game.platform,resources=realms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=game.platform,resources=realms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=game.platform,resources=capabilitybindings,verbs=get;list;watch;create;update;patch;delete
type RealmReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *RealmReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("controller", "Realm", "realm", req.Name)

	var realm gamev1alpha1.Realm
	if err := r.Get(ctx, req.NamespacedName, &realm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// For each module in the Realm spec, ensure a CapabilityBinding exists.
	// These bindings are "root" bindings for the Realm scope.
	for _, mod := range realm.Spec.Modules {
		bindingName := fmt.Sprintf("realm-%s-%s", realm.Name, mod.Name)
		bindingName = strings.ToLower(bindingName)

		// Ensure binding
		binding := &gamev1alpha1.CapabilityBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: realm.Namespace,
			},
		}

		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, binding, func() error {
			binding.Labels = map[string]string{
				rtLabelManagedBy:      realmManagedBy,
				"game.platform/realm": realm.Name,
			}

			// Spec
			binding.Spec.CapabilityID = "system.root" // Synthetic root capability
			binding.Spec.Scope = gamev1alpha1.CapabilityScopeRealm
			binding.Spec.Multiplicity = gamev1alpha1.MultiplicityOne
			// Consumer is the Realm itself
			binding.Spec.Consumer = gamev1alpha1.ConsumerRef{
				ModuleManifestName: "realm-" + realm.Name,
			}

			// Provider is the module
			binding.Spec.Provider = gamev1alpha1.ProviderRef{
				ModuleManifestName: mod.Name,
				CapabilityVersion:  mod.Version,
			}

			return controllerutil.SetControllerReference(&realm, binding, r.Scheme)
		})

		if err != nil {
			logger.Error(err, "failed to ensure realm binding", "binding", bindingName)
			return ctrl.Result{}, err
		}
	}

	// TODO: Garbage collect bindings for modules removed from Realm spec.

	return ctrl.Result{}, nil
}

func (r *RealmReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gamev1alpha1.Realm{}).
		Owns(&gamev1alpha1.CapabilityBinding{}).
		Complete(r)
}
