package controllers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/cli-utils/pkg/object"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gitops-tools/pkg/sets"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates"
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
	logger := log.FromContext(ctx)
	var gitOpsSet templatesv1.GitOpsSet
	if err := r.Client.Get(ctx, req.NamespacedName, &gitOpsSet); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("gitops set loaded")

	if !gitOpsSet.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	inventory, err := r.reconcileResources(ctx, &gitOpsSet)
	if err != nil {
		return ctrl.Result{}, err
	}

	if inventory != nil {
		gitOpsSet = templatesv1.GitOpsSetReady(gitOpsSet, inventory, templatesv1.HealthyCondition, fmt.Sprintf("%d resources created", len(inventory.Entries)))
		if err := r.Status().Update(ctx, &gitOpsSet); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *GitOpsSetReconciler) reconcileResources(ctx context.Context, gitOpsSet *templatesv1.GitOpsSet) (*templatesv1.ResourceInventory, error) {
	generators := map[string]generators.Generator{}
	for k, factory := range r.Generators {
		generators[k] = factory(log.FromContext(ctx))
	}

	resources, err := templates.Render(ctx, gitOpsSet, generators)
	if err != nil {
		return nil, err
	}

	existingEntries := sets.New[templatesv1.ResourceRef]()
	if gitOpsSet.Status.Inventory != nil {
		existingEntries.Insert(gitOpsSet.Status.Inventory.Entries...)
	}

	entries := sets.New[templatesv1.ResourceRef]()
	for _, resource := range resources {
		objMeta, err := object.RuntimeToObjMeta(resource)
		if err != nil {
			return nil, fmt.Errorf("failed to update inventory: %w", err)
		}
		ref := templatesv1.ResourceRef{
			ID:      objMeta.String(),
			Version: resource.GetObjectKind().GroupVersionKind().GroupVersion().String(),
		}
		entries.Insert(ref)

		// if existingEntries.Has(ref) {
		// 	existing := &kustomizev1.Kustomization{}
		// 	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(resource), existing); err != nil {
		// 		return nil, fmt.Errorf("failed to load existing Kustomization: %w", err)
		// 	}
		// 	patchHelper, err := patch.NewHelper(existing, r.Client)
		// 	if err != nil {
		// 		return nil, fmt.Errorf("failed to create patch helper for Kustomization: %w", err)
		// 	}
		// 	existing.ObjectMeta.Annotations = resource.Annotations
		// 	existing.ObjectMeta.Labels = resource.Labels
		// 	existing.Spec = resource.Spec
		// 	if err := patchHelper.Patch(ctx, existing); err != nil {
		// 		return nil, fmt.Errorf("failed to update Kustomization: %w", err)
		// 	}
		// 	continue
		// }

		controllerutil.SetOwnerReference(gitOpsSet, resource, r.Scheme)

		if err := r.Client.Create(ctx, resource); err != nil {
			return nil, fmt.Errorf("failed to create Resource: %w", err)
		}
	}

	if gitOpsSet.Status.Inventory == nil {
		return &templatesv1.ResourceInventory{Entries: entries.SortedList(func(x, y templatesv1.ResourceRef) bool {
			return x.ID < y.ID
		})}, nil

	}
	// kustomizationsToRemove := existingEntries.Difference(entries)
	// if err := r.removeResourceRefs(ctx, kustomizationsToRemove.List()); err != nil {
	// 	return nil, err
	// }

	return &templatesv1.ResourceInventory{Entries: entries.SortedList(func(x, y templatesv1.ResourceRef) bool {
		return x.ID < y.ID
	})}, nil

}

// func (r *GitOpsSetReconciler) removeResourceRefs(ctx context.Context, deletions []templatesv1.ResourceRef) error {
// 	for _, v := range deletions {
// 		objMeta, err := object.ParseObjMetadata(v.ID)
// 		if err != nil {
// 			return fmt.Errorf("failed to parse object ID %s for deletion: %w", v.ID, err)
// 		}
// 		k := kustomizev1.Kustomization{
// 			ObjectMeta: metav1.ObjectMeta{
// 				Name:      objMeta.Name,
// 				Namespace: objMeta.Namespace,
// 			},
// 		}
// 		if err := r.Client.Delete(ctx, &k); err != nil {
// 			return fmt.Errorf("failed to delete %v: %w", k, err)
// 		}
// 	}
// 	return nil
// }

// SetupWithManager sets up the controller with the Manager.
func (r *GitOpsSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&templatesv1.GitOpsSet{}).
		Complete(r)
}
