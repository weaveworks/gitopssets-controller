package controllers

import (
	"context"
	"fmt"
	"sort"
	"time"

	// TODO: v0.26.0 api has support for a generic Set, switch to this
	// when Flux supports v0.26.0
	"github.com/gitops-tools/pkg/sets"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	fluxMeta "github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	runtimeCtrl "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/patch"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
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

	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
)

var accessor = meta.NewAccessor()

const (
	gitRepositoryIndexKey string = ".metadata.gitRepository"
)

type eventRecorder interface {
	Event(object runtime.Object, eventtype, reason, message string)
}

// GitOpsSetReconciler reconciles a GitOpsSet object
type GitOpsSetReconciler struct {
	client.Client
	DefaultServiceAccount string
	Config                *rest.Config
	EventRecorder         eventRecorder
	runtimeCtrl.Metrics

	Generators map[string]generators.GeneratorFactory

	Scheme *runtime.Scheme
	Mapper meta.RESTMapper
}

// event emits a Kubernetes event using EventRecorder
func (r *GitOpsSetReconciler) event(obj *templatesv1.GitOpsSet, severity, msg string, metadata map[string]string) {
	if metadata == nil {
		metadata = map[string]string{}
	}

	reason := severity
	conditions.GetReason(obj, fluxMeta.ReadyCondition)
	if r := conditions.GetReason(obj, fluxMeta.ReadyCondition); r != "" {
		reason = r
	}

	eventtype := "Normal"
	if severity == eventv1.EventSeverityError {
		eventtype = "Error"
	}

	r.EventRecorder.Event(obj, eventtype, reason, msg)
}

//+kubebuilder:rbac:groups=templates.weave.works,resources=gitopssets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=templates.weave.works,resources=gitopssets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=templates.weave.works,resources=gitopssets/finalizers,verbs=update
//+kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=impersonate
//+kubebuilder:rbac:groups=gitops.weave.works,resources=gitopsclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.1/pkg/reconcile
func (r *GitOpsSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	logger := log.FromContext(ctx)
	reconcileStart := time.Now()

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

	// Set the value of the reconciliation request in status.
	if v, ok := fluxMeta.ReconcileAnnotationValue(gitOpsSet.GetAnnotations()); ok {
		gitOpsSet.Status.LastHandledReconcileAt = v
	}

	k8sClient := r.Client
	if gitOpsSet.Spec.ServiceAccountName != "" || r.DefaultServiceAccount != "" {
		serviceAccountName := r.DefaultServiceAccount
		if gitOpsSet.Spec.ServiceAccountName != "" {
			serviceAccountName = gitOpsSet.Spec.ServiceAccountName
		}
		c, err := r.makeImpersonationClient(gitOpsSet.Namespace, serviceAccountName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create client for ServiceAccount %s: %w", serviceAccountName, err)
		}
		k8sClient = c
	}

	defer func() {
		// Record Prometheus metrics.
		r.Metrics.RecordReadiness(ctx, &gitOpsSet)
		r.Metrics.RecordDuration(ctx, &gitOpsSet, reconcileStart)
		r.Metrics.RecordSuspend(ctx, &gitOpsSet, gitOpsSet.Spec.Suspend)

		// Log and emit success event.
		if r.EventRecorder != nil && templatesv1.GetGitOpsSetReadiness(&gitOpsSet) == metav1.ConditionTrue {
			msg := fmt.Sprintf("Reconciliation finished in %s",
				time.Since(reconcileStart).String())
			r.event(&gitOpsSet, eventv1.EventSeverityInfo, msg,
				map[string]string{
					templatesv1.GroupVersion.Group + "/" + eventv1.MetaCommitStatusKey: eventv1.MetaCommitStatusUpdateValue,
				})
		}
	}()

	inventory, requeue, err := r.reconcileResources(ctx, k8sClient, &gitOpsSet)

	if err != nil {
		templatesv1.SetGitOpsSetReadiness(&gitOpsSet, metav1.ConditionFalse, templatesv1.ReconciliationFailedReason, err.Error())
		if err := r.patchStatus(ctx, req, gitOpsSet.Status); err != nil {
			logger.Error(err, "failed to reconcile")
		}
		msg := fmt.Sprintf("Reconciliation failed after %s", time.Since(reconcileStart).String())
		r.event(&gitOpsSet, eventv1.EventSeverityError, msg,
			map[string]string{
				templatesv1.GroupVersion.Group + "/" + eventv1.MetaCommitStatusKey: eventv1.MetaCommitStatusUpdateValue,
			})

		return ctrl.Result{}, err
	}

	if inventory != nil {
		templatesv1.SetReadyWithInventory(&gitOpsSet, inventory, templatesv1.ReconciliationSucceededReason,
			fmt.Sprintf("%d resources created", len(inventory.Entries)))

		if err := r.patchStatus(ctx, req, gitOpsSet.Status); err != nil {
			templatesv1.SetGitOpsSetReadiness(&gitOpsSet, metav1.ConditionFalse, templatesv1.ReconciliationFailedReason, err.Error())
			logger.Error(err, "failed to reconcile")
			msg := fmt.Sprintf("Status and inventory update failed after reconciliation")
			r.event(&gitOpsSet, eventv1.EventSeverityError, msg,
				map[string]string{
					templatesv1.GroupVersion.Group + "/" + eventv1.MetaCommitStatusKey: eventv1.MetaCommitStatusUpdateValue,
				})
			return ctrl.Result{}, fmt.Errorf("failed to update status and inventory: %w", err)
		}
	}

	return ctrl.Result{RequeueAfter: requeue}, nil
}

func (r *GitOpsSetReconciler) reconcileResources(ctx context.Context, k8sClient client.Client, gitOpsSet *templatesv1.GitOpsSet) (*templatesv1.ResourceInventory, time.Duration, error) {
	logger := log.FromContext(ctx)
	instantiatedGenerators := map[string]generators.Generator{}
	for k, factory := range r.Generators {
		instantiatedGenerators[k] = factory(log.FromContext(ctx), r.Client)
	}

	inventory, err := r.renderAndReconcile(ctx, logger, k8sClient, gitOpsSet, instantiatedGenerators)
	if err != nil {
		return nil, generators.NoRequeueInterval, err
	}

	requeueAfter := calculateInterval(gitOpsSet, instantiatedGenerators)

	return inventory, requeueAfter, nil
}

func (r *GitOpsSetReconciler) renderAndReconcile(ctx context.Context, logger logr.Logger, k8sClient client.Client, gitOpsSet *templatesv1.GitOpsSet, instantiatedGenerators map[string]generators.Generator) (*templatesv1.ResourceInventory, error) {
	resources, err := templates.Render(ctx, gitOpsSet, instantiatedGenerators)
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
		ref, err := templatesv1.ResourceRefFromObject(newResource)
		if err != nil {
			return nil, fmt.Errorf("failed to update inventory: %w", err)
		}
		entries.Insert(ref)

		if existingEntries.Has(ref) {
			existing, err := unstructuredFromResourceRef(ref)
			if err != nil {
				return nil, fmt.Errorf("failed to convert resource for update: %w", err)
			}
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(newResource), existing)
			if err == nil {
				patchHelper, err := patch.NewHelper(existing, k8sClient)
				if err != nil {
					return nil, fmt.Errorf("failed to create patch helper for Resource: %w", err)
				}

				if err := logResourceMessage(logger, "updating existing resource if necessary", newResource); err != nil {
					return nil, err
				}
				existing = copyUnstructuredContent(existing, newResource)
				if err := patchHelper.Patch(ctx, existing); err != nil {
					return nil, fmt.Errorf("failed to update Resource: %w", err)
				}
				continue
			}

			if !apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("failed to load existing Resource: %w", err)
			}
		}

		// cluster-scoped resources must not have a namespace-scoped owner
		if templates.IsNamespacedObject(newResource) {
			if err := controllerutil.SetOwnerReference(gitOpsSet, newResource, r.Scheme); err != nil {
				return nil, fmt.Errorf("failed to set ownership: %w", err)
			}
		}

		if err := logResourceMessage(logger, "creating new resource", newResource); err != nil {
			return nil, err
		}

		if err := k8sClient.Create(ctx, newResource); err != nil {
			return nil, fmt.Errorf("failed to create Resource: %w", err)
		}
	}

	if gitOpsSet.Status.Inventory == nil {
		return &templatesv1.ResourceInventory{Entries: entries.SortedList(func(x, y templatesv1.ResourceRef) bool {
			return x.ID < y.ID
		})}, nil

	}
	objectsToRemove := existingEntries.Difference(entries)
	if err := r.removeResourceRefs(ctx, k8sClient, objectsToRemove.List()); err != nil {
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

func (r *GitOpsSetReconciler) removeResourceRefs(ctx context.Context, k8sClient client.Client, deletions []templatesv1.ResourceRef) error {
	logger := log.FromContext(ctx)
	for _, v := range deletions {
		u, err := unstructuredFromResourceRef(v)
		if err != nil {
			return err
		}
		if err := logResourceMessage(logger, "deleting resource", u); err != nil {
			return err
		}

		if err := k8sClient.Delete(ctx, u); err != nil {
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

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&templatesv1.GitOpsSet{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&source.Kind{Type: &sourcev1.GitRepository{}},
			handler.EnqueueRequestsFromMapFunc(r.gitRepositoryToGitOpsSet),
		)

	// Only watch for GitopsCluster objects if the Cluster generator is enabled.
	if r.Generators["Cluster"] != nil {
		builder.Watches(
			&source.Kind{Type: &clustersv1.GitopsCluster{}},
			handler.EnqueueRequestsFromMapFunc(r.gitOpsClusterToGitOpsSet),
		)
	}

	return builder.Complete(r)
}

// gitOpsClusterToGitOpsSet maps a GitopsCluster object to its related GitOpsSet objects
// and returns a list of reconcile requests for the GitOpsSets.
func (r *GitOpsSetReconciler) gitOpsClusterToGitOpsSet(o client.Object) []reconcile.Request {
	gitOpsCluster, ok := o.(*clustersv1.GitopsCluster)
	if !ok {
		return nil
	}

	ctx := context.Background()
	list := &templatesv1.GitOpsSetList{}

	err := r.List(ctx, list, &client.ListOptions{})
	if err != nil {
		return nil
	}

	var result []reconcile.Request
	for _, v := range list.Items {
		if matchCluster(gitOpsCluster, &v) {
			result = append(result, reconcile.Request{NamespacedName: types.NamespacedName{Name: v.GetName(), Namespace: v.GetNamespace()}})
		}
	}

	return result
}

func matchCluster(gitOpsCluster *clustersv1.GitopsCluster, gitOpsSet *templatesv1.GitOpsSet) bool {
	for _, generator := range gitOpsSet.Spec.Generators {
		for _, selector := range getClusterSelectors(generator) {
			if selectorMatchesCluster(selector, gitOpsCluster) {
				return true
			}
		}
	}

	return false
}

func getClusterSelectors(generator templatesv1.GitOpsSetGenerator) []metav1.LabelSelector {
	selectors := []metav1.LabelSelector{}

	if generator.Cluster != nil {
		selectors = append(selectors, generator.Cluster.Selector)
	}

	if generator.Matrix != nil && generator.Matrix.Generators != nil {
		for _, matrixGenerator := range generator.Matrix.Generators {
			if matrixGenerator.Cluster != nil {
				selectors = append(selectors, matrixGenerator.Cluster.Selector)
			}
		}
	}

	return selectors
}

func selectorMatchesCluster(labelSelector metav1.LabelSelector, cluster *clustersv1.GitopsCluster) bool {
	selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
	if err != nil {
		return false
	}

	// If the selector is empty, then we don't match anything.
	// We want to be cautious here, so we don't accidentally match
	// all clusters.
	if selector.Empty() {
		return false
	}

	labelSet := labels.Set(cluster.GetLabels())

	return selector.Matches(labelSet)
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

func (r *GitOpsSetReconciler) makeImpersonationClient(namespace, serviceAccountName string) (client.Client, error) {
	copyCfg := rest.CopyConfig(r.Config)

	copyCfg.Impersonate = rest.ImpersonationConfig{
		UserName: fmt.Sprintf("system:serviceaccount:%s:%s", namespace, serviceAccountName),
	}

	return client.New(copyCfg, client.Options{Scheme: r.Scheme, Mapper: r.Mapper})
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

func calculateInterval(gs *templatesv1.GitOpsSet, configuredGenerators map[string]generators.Generator) time.Duration {
	res := []time.Duration{}
	for _, mg := range gs.Spec.Generators {
		relevantGenerators := generators.FindRelevantGenerators(mg, configuredGenerators)

		for _, rg := range relevantGenerators {
			d := rg.Interval(&mg)

			if d > generators.NoRequeueInterval {
				res = append(res, d)
			}

		}
	}

	if len(res) == 0 {
		return generators.NoRequeueInterval
	}

	// Find the lowest requeue interval provided by a generator.
	sort.Slice(res, func(i, j int) bool { return res[i] < res[j] })

	return res[0]
}
