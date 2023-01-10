package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	templatesv1alpha1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
)

// GitOpsSetReconciler reconciles a GitOpsSet object
type GitOpsSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	Generators map[string]generators.GeneratorFactory
}

//+kubebuilder:rbac:groups=templates.weave.works,resources=gitopssets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=templates.weave.works,resources=gitopssets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=templates.weave.works,resources=gitopssets/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.1/pkg/reconcile
func (r *GitOpsSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitOpsSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&templatesv1alpha1.GitOpsSet{}).
		Complete(r)
}
