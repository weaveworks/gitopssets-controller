package controllers

import (
	"context"
	"fmt"

	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/go-logr/logr"

	// TODO: v0.26.0 api has support for a generic Set, switch to this
	// when Flux supports v0.26.0
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/gitops-tools/pkg/sets"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cli-utils/pkg/object"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
)

var accessor = meta.NewAccessor()

const (
	gitRepositoryIndexKey string = ".metadata.gitRepository"
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
//+kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.1/pkg/reconcile
func (r *GitOpsSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	logger := log.FromContext(ctx)

	var gitOpsSet templatesv1.GitOpsSet
	if err := r.Client.Get(ctx, req.NamespacedName, &gitOpsSet); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("gitops set loaded")

	if !gitOpsSet.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Skip reconciliation if the GitOpsSet is suspended.
	if gitOpsSet.Spec.Suspend {
		logger.Info("Reconciliation is suspended for this GitOpsSet")
		return ctrl.Result{}, nil
	}

	inventory, err := r.reconcileResources(ctx, &gitOpsSet)
	if err != nil {
		templatesv1.SetGitOpsSetReadiness(&gitOpsSet, metav1.ConditionFalse, templatesv1.ReconciliationFailedReason, err.Error())
		if err := r.patchStatus(ctx, req, gitOpsSet.Status); err != nil {
			logger.Error(err, "failed to reconcile")
		}

		return ctrl.Result{}, err
	}

	if inventory != nil {
		templatesv1.SetReadyWithInventory(&gitOpsSet, inventory, templatesv1.ReconciliationSucceededReason,
			fmt.Sprintf("%d resources created", len(inventory.Entries)))

		if err := r.patchStatus(ctx, req, gitOpsSet.Status); err != nil {
			logger.Error(err, "failed to reconcile")
			return ctrl.Result{}, fmt.Errorf("failed to update status and inventory: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

func (r *GitOpsSetReconciler) reconcileResources(ctx context.Context, gitOpsSet *templatesv1.GitOpsSet) (*templatesv1.ResourceInventory, error) {
	logger := log.FromContext(ctx)
	generators := map[string]generators.Generator{}
	for k, factory := range r.Generators {
		generators[k] = factory(log.FromContext(ctx), r.Client)
	}

	resources, err := templates.Render(ctx, gitOpsSet, generators)
	if err != nil {
		return nil, err
	}
	logger.Info("rendered templates", "resourceCount", len(resources))

	existingEntries := sets.New[templatesv1.ResourceRef]()
	if gitOpsSet.Status.Inventory != nil {
		existingEntries.Insert(gitOpsSet.Status.Inventory.Entries...)
	}

	entries := sets.New[templatesv1.ResourceRef]()
	for _, newResource := range resources {
		ref, err := resourceRefFromObject(newResource)
		if err != nil {
			return nil, fmt.Errorf("failed to update inventory: %w", err)
		}
		entries.Insert(ref)

		if existingEntries.Has(ref) {
			existing, err := unstructuredFromResourceRef(ref)
			if err != nil {
				return nil, fmt.Errorf("failed to convert resource for update: %w", err)
			}
			if err := r.Client.Get(ctx, client.ObjectKeyFromObject(newResource), existing); err != nil {
				return nil, fmt.Errorf("failed to load existing Resource: %w", err)
			}
			patchHelper, err := patch.NewHelper(existing, r.Client)
			if err != nil {
				return nil, fmt.Errorf("failed to create patch helper for Resource: %w", err)
			}

			if err := logResourceMessage(logger, "updating existing resource", newResource); err != nil {
				return nil, err
			}
			existing = copyUnstructuredContent(existing, newResource)
			if err := patchHelper.Patch(ctx, existing); err != nil {
				return nil, fmt.Errorf("failed to update Resource: %w", err)
			}
			continue
		}

		if err := controllerutil.SetOwnerReference(gitOpsSet, newResource, r.Scheme); err != nil {
			return nil, fmt.Errorf("failed to set ownership: %w", err)
		}

		if err := logResourceMessage(logger, "creating new resource", newResource); err != nil {
			return nil, err
		}

		if err := r.Client.Create(ctx, newResource); err != nil {
			return nil, fmt.Errorf("failed to create Resource: %w", err)
		}
	}

	if gitOpsSet.Status.Inventory == nil {
		return &templatesv1.ResourceInventory{Entries: entries.SortedList(func(x, y templatesv1.ResourceRef) bool {
			return x.ID < y.ID
		})}, nil

	}
	objectsToRemove := existingEntries.Difference(entries)
	if err := r.removeResourceRefs(ctx, objectsToRemove.List()); err != nil {
		return nil, err
	}

	return &templatesv1.ResourceInventory{Entries: entries.SortedList(func(x, y templatesv1.ResourceRef) bool {
		return x.ID < y.ID
	})}, nil

}

func (r *GitOpsSetReconciler) patchStatus(ctx context.Context, req ctrl.Request, newStatus templatesv1.GitOpsSetStatus) error {
	var set templatesv1.GitOpsSet
	if err := r.Get(ctx, req.NamespacedName, &set); err != nil {
		return err
	}

	patch := client.MergeFrom(set.DeepCopy())
	set.Status = newStatus

	return r.Status().Patch(ctx, &set, patch)
}

func (r *GitOpsSetReconciler) removeResourceRefs(ctx context.Context, deletions []templatesv1.ResourceRef) error {
	logger := log.FromContext(ctx)
	for _, v := range deletions {
		u, err := unstructuredFromResourceRef(v)
		if err != nil {
			return err
		}
		if err := logResourceMessage(logger, "deleting resource", u); err != nil {
			return err
		}

		if err := r.Client.Delete(ctx, u); err != nil {
			return fmt.Errorf("failed to delete %v: %w", u, err)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitOpsSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Index the GitOpsSets by the GitRepository references they (may) point at.
	if err := mgr.GetCache().IndexField(
		context.TODO(), &templatesv1.GitOpsSet{}, gitRepositoryIndexKey, indexGitRepositories); err != nil {
		return fmt.Errorf("failed setting index fields: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&templatesv1.GitOpsSet{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&source.Kind{Type: &sourcev1.GitRepository{}},
			handler.EnqueueRequestsFromMapFunc(r.gitRepositoryToGitOpsSet),
		).
		Complete(r)
}

func (r *GitOpsSetReconciler) gitRepositoryToGitOpsSet(obj client.Object) []reconcile.Request {
	// TODO: Store the applied version of GitRepositories in the Status, and don't
	// retrigger if the commit-id isn't different.
	ctx := context.Background()
	var list templatesv1.GitOpsSetList

	if err := r.List(ctx, &list, client.MatchingFields{
		gitRepositoryIndexKey: client.ObjectKeyFromObject(obj).String(),
	}); err != nil {
		return nil
	}

	result := []reconcile.Request{}
	for _, v := range list.Items {
		result = append(result, reconcile.Request{NamespacedName: types.NamespacedName{Name: v.GetName(), Namespace: v.GetNamespace()}})
	}

	return result
}

func indexGitRepositories(o client.Object) []string {
	ks, ok := o.(*templatesv1.GitOpsSet)
	if !ok {
		panic(fmt.Sprintf("Expected a GitOpsSet, got %T", o))
	}

	referencedRepositories := []*templatesv1.GitRepositoryGenerator{}
	for _, gen := range ks.Spec.Generators {
		if gen.GitRepository != nil {
			referencedRepositories = append(referencedRepositories, gen.GitRepository)
		}
	}

	if len(referencedRepositories) == 0 {
		return nil
	}

	referencedNames := []string{}
	for _, grg := range referencedRepositories {
		referencedNames = append(referencedNames, fmt.Sprintf("%s/%s", ks.GetNamespace(), grg.RepositoryRef))
	}

	return referencedNames
}

func unstructuredFromResourceRef(ref templatesv1.ResourceRef) (*unstructured.Unstructured, error) {
	objMeta, err := object.ParseObjMetadata(ref.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse object ID %s: %w", ref.ID, err)
	}
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(objMeta.GroupKind.WithVersion(ref.Version))
	u.SetName(objMeta.Name)
	u.SetNamespace(objMeta.Namespace)

	return &u, nil
}

func resourceRefFromObject(obj runtime.Object) (templatesv1.ResourceRef, error) {
	objMeta, err := object.RuntimeToObjMeta(obj)
	if err != nil {
		return templatesv1.ResourceRef{}, fmt.Errorf("failed to parse object Metadata: %w", err)
	}

	return templatesv1.ResourceRef{
		ID:      objMeta.String(),
		Version: obj.GetObjectKind().GroupVersionKind().Version,
	}, nil
}

func copyUnstructuredContent(existing, newValue *unstructured.Unstructured) *unstructured.Unstructured {
	result := unstructured.Unstructured{}
	existing.DeepCopyInto(&result)

	disallowedKeys := sets.New("status", "metadata", "kind", "apiVersion")

	for k, v := range newValue.Object {
		if !disallowedKeys.Has(k) {
			result.Object[k] = v
		}
	}

	result.SetAnnotations(newValue.GetAnnotations())
	result.SetLabels(newValue.GetLabels())

	return &result
}

func logResourceMessage(logger logr.Logger, msg string, obj runtime.Object) error {
	namespace, err := accessor.Namespace(obj)
	if err != nil {
		return err
	}
	name, err := accessor.Name(obj)
	if err != nil {
		return err
	}
	kind, err := accessor.Kind(obj)
	if err != nil {
		return err
	}

	logger.Info(msg, "objNamespace", namespace, "objName", name, "kind", kind)

	return nil

}
