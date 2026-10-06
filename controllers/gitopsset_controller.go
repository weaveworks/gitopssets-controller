package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1"
	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	fluxMeta "github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	runtimeCtrl "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/predicates"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
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

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
)

var accessor = meta.NewAccessor()

const (
	gitRepositoryIndexKey    string = ".metadata.gitRepository"
	ociRepositoryIndexKey    string = ".metadata.ociRepository"
	imagePolicyIndexKey      string = ".metadata.imagePolicy"
	configMapIndexKey        string = ".metadata.configMap"
	secretIndexKey           string = ".metadata.secret"
	clusterGeneratorIndexKey string = ".metadata.clusterGenerator"
)

type eventRecorder interface {
	Eventf(object runtime.Object, related runtime.Object, eventtype, reason string, action string, messageFmt string, args ...any)
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

	// impersonationClient, when set, replaces the Kubernetes impersonation
	// config. Tests use it to supply one client per service account.
	impersonationClient func(namespace, serviceAccountName string) (client.Client, error)
}

// event emits a Kubernetes event using EventRecorder
func (r *GitOpsSetReconciler) event(obj *templatesv1.GitOpsSet, severity, msg string, args ...any) {
	reason := conditions.GetReason(obj, fluxMeta.ReadyCondition)
	if reason == "" {
		reason = severity
	}

	eventType := corev1.EventTypeNormal
	if severity == eventv1.EventSeverityError {
		eventType = corev1.EventTypeWarning
	}

	r.EventRecorder.Eventf(obj, nil, eventType, reason, "", "%s", truncateEventMessage(fmt.Sprintf(msg, args...)))
}

const maxEventMessageLength = 1024

func truncateEventMessage(msg string) string {
	const suffix = "...(truncated)"
	if len(msg) <= maxEventMessageLength {
		return msg
	}
	return msg[:maxEventMessageLength-len(suffix)] + suffix
}

//+kubebuilder:rbac:groups=sets.gitops.pro,resources=gitopssets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=sets.gitops.pro,resources=gitopssets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=sets.gitops.pro,resources=gitopssets/finalizers,verbs=update
//+kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch
//+kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=ocirepositories,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=impersonate
//+kubebuilder:rbac:groups=gitops.weave.works,resources=gitopsclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imagepolicies,verbs=get;list;watch
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

	logger.Info("GitOpsSet loaded")

	// Add finalizer first if it doesn't exist to avoid the race condition
	// between init and delete.
	if !controllerutil.ContainsFinalizer(&gitOpsSet, templatesv1.GitOpsSetFinalizer) {
		controllerutil.AddFinalizer(&gitOpsSet, templatesv1.GitOpsSetFinalizer)

		if err := r.Update(ctx, &gitOpsSet); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{Requeue: true}, nil
	}

	stats := &applyStats{}
	ctx = context.WithValue(ctx, applyStatsKey, stats)
	previousHealthy := ""
	if cond := meta.FindStatusCondition(gitOpsSet.Status.Conditions, templatesv1.HealthyCondition); cond != nil {
		previousHealthy = cond.Reason
	}

	defer func() {
		// Record Prometheus metrics.
		r.Metrics.RecordReadiness(ctx, &gitOpsSet)
		r.Metrics.RecordDuration(ctx, &gitOpsSet, reconcileStart)
		r.Metrics.RecordSuspend(ctx, &gitOpsSet, gitOpsSet.Spec.Suspend)

		// A pass that only rechecks health or finds unchanged objects does not
		// emit a success event.
		if r.EventRecorder != nil && stats.patched && templatesv1.GetGitOpsSetReadiness(&gitOpsSet) == metav1.ConditionTrue {
			r.event(&gitOpsSet, eventv1.EventSeverityInfo, "Reconciliation finished in %s: %s",
				time.Since(reconcileStart).String(), conditions.GetMessage(&gitOpsSet, fluxMeta.ReadyCondition))
		}
	}()

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

	if !gitOpsSet.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, &gitOpsSet, k8sClient)
	}

	if gitOpsSet.Spec.Suspend {
		logger.Info("Reconciliation is suspended for this GitOpsSet")
		templatesv1.SetGitOpsSetReadiness(&gitOpsSet, nil, metav1.ConditionFalse, templatesv1.SuspendedReason, "reconciliation is suspended")
		if err := r.patchStatus(ctx, req, gitOpsSet.Status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Set the value of the reconciliation request in status.
	if v, ok := fluxMeta.ReconcileAnnotationValue(gitOpsSet.GetAnnotations()); ok {
		gitOpsSet.Status.LastHandledReconcileAt = v
	}

	inventory, requeue, err := r.reconcileResources(ctx, k8sClient, &gitOpsSet)

	var progress *rolloutProgressError
	if errors.As(err, &progress) {
		if requeue == 0 {
			requeue = 10 * time.Second
		}
		templatesv1.SetGitOpsSetReadiness(&gitOpsSet, inventory, metav1.ConditionFalse, templatesv1.RolloutProgressingReason, err.Error())
		if patchErr := r.patchStatus(ctx, req, gitOpsSet.Status); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{RequeueAfter: requeue}, nil
	}

	var retry *retryAfterError
	if errors.As(err, &retry) {
		templatesv1.SetGitOpsSetReadiness(&gitOpsSet, inventory, metav1.ConditionFalse, templatesv1.WaitingForNamespaceReason, err.Error())
		if patchErr := r.patchStatus(ctx, req, gitOpsSet.Status); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{RequeueAfter: retry.after}, nil
	}

	if err != nil {
		// We can return here because when the resource artifact is updated, this
		// will trigger a reconciliation.

		if errors.As(err, &generators.NoArtifactError{}) {
			templatesv1.SetGitOpsSetReadiness(&gitOpsSet, inventory, metav1.ConditionFalse, templatesv1.ReconciliationFailedReason, "waiting for artifact")
			if err := r.patchStatus(ctx, req, gitOpsSet.Status); err != nil {
				logger.Error(err, "failed to reconcile")
			}
			return ctrl.Result{}, nil
		}

		templatesv1.SetGitOpsSetReadiness(&gitOpsSet, inventory, metav1.ConditionFalse, templatesv1.ReconciliationFailedReason, err.Error())
		if err := r.patchStatus(ctx, req, gitOpsSet.Status); err != nil {
			logger.Error(err, "failed to reconcile")
		}
		r.event(&gitOpsSet, eventv1.EventSeverityError, "Reconciliation failed after %s: %s", time.Since(reconcileStart).String(), err.Error())

		return ctrl.Result{}, err
	}

	if inventory != nil {
		templatesv1.SetGitOpsSetReadiness(&gitOpsSet, inventory, metav1.ConditionTrue, templatesv1.ReconciliationSucceededReason,
			fmt.Sprintf("%d resources created", len(inventory.Entries)))

		healthAfter := r.observeHealth(ctx, k8sClient, &gitOpsSet, inventory)
		if len(stats.waiting) > 0 {
			message := waitingMessage(stats.waiting)
			setHealthy(&gitOpsSet, metav1.ConditionFalse, templatesv1.WaitingForDependencyReason, message)
			healthAfter = healthRequeueInterval
			if r.EventRecorder != nil && previousHealthy != templatesv1.WaitingForDependencyReason {
				r.event(&gitOpsSet, eventv1.EventSeverityInfo, "waiting for dependency: %s", message)
			}
		}
		if healthAfter > 0 && (requeue == 0 || healthAfter < requeue) {
			requeue = healthAfter
		}

		if err := r.patchStatus(ctx, req, gitOpsSet.Status); err != nil {
			logger.Error(err, "failed to reconcile")
			templatesv1.SetGitOpsSetReadiness(&gitOpsSet, inventory, metav1.ConditionFalse, templatesv1.ReconciliationFailedReason, err.Error())
			r.event(&gitOpsSet, eventv1.EventSeverityError, "Status and inventory update failed after reconciliation")

			return ctrl.Result{}, fmt.Errorf("failed to update status and inventory: %w", err)
		}
	}

	return ctrl.Result{RequeueAfter: requeue}, nil
}

func (r *GitOpsSetReconciler) reconcileResources(ctx context.Context, k8sClient client.Client, gitOpsSet *templatesv1.GitOpsSet) (*templatesv1.ResourceInventory, time.Duration, error) {
	logger := log.FromContext(ctx)
	sources, sourcesComplete, err := r.snapshotSources(ctx, k8sClient, gitOpsSet)
	if err != nil {
		return nil, generators.NoRequeueInterval, err
	}
	instantiatedGenerators := generatorMap(ctx, r, k8sClient)
	requeueAfter, err := calculateInterval(gitOpsSet, instantiatedGenerators)
	if err != nil {
		return nil, generators.NoRequeueInterval, fmt.Errorf("failed to calculate requeue interval: %w", err)
	}
	if canSkipApply(gitOpsSet, sources, sourcesComplete) {
		logger.Info("skipping render because sources are unchanged")
		return gitOpsSet.Status.Inventory, requeueAfter, nil
	}

	inventory, err := r.renderAndReconcile(ctx, logger, k8sClient, gitOpsSet, instantiatedGenerators)
	if err != nil {
		var progress *rolloutProgressError
		if errors.As(err, &progress) {
			if requeueAfter == 0 {
				requeueAfter = 10 * time.Second
			}
			return inventory, requeueAfter, err
		}
		return inventory, generators.NoRequeueInterval, err
	}
	if sourcesComplete && inventory != nil {
		revision := appliedSourceRevision(sources)
		for i := range inventory.Entries {
			if inventory.Entries[i].LastError == "" {
				inventory.Entries[i].SourceRevision = revision
			}
		}
		gitOpsSet.Status.LastAppliedSources = sources
	}

	return inventory, requeueAfter, nil
}

func generatorMap(ctx context.Context, r *GitOpsSetReconciler, k8sClient client.Client) map[string]generators.Generator {
	instantiated := map[string]generators.Generator{}
	for name, factory := range r.Generators {
		instantiated[name] = factory(log.FromContext(ctx), k8sClient)
	}
	return instantiated
}

func (r *GitOpsSetReconciler) renderAndReconcile(ctx context.Context, logger logr.Logger, k8sClient client.Client, gitOpsSet *templatesv1.GitOpsSet, instantiatedGenerators map[string]generators.Generator) (*templatesv1.ResourceInventory, error) {
	rendered, err := templates.RenderObjects(ctx, gitOpsSet, instantiatedGenerators, r.Mapper)
	if err != nil {
		return nil, err
	}
	rendered, err = orderRendered(r.Mapper, rendered)
	if err != nil {
		return nil, err
	}
	logger.Info("rendered templates", "resourceCount", len(rendered))

	clients := newClientSet(r, gitOpsSet, k8sClient)
	var inventoryErr error
	budget := rolloutBudget(gitOpsSet)

	existingEntries := newResourceRefSet(nil)
	if gitOpsSet.Status.Inventory != nil {
		existingEntries = newResourceRefSet(gitOpsSet.Status.Inventory.Entries)
	}
	desired := newResourceRefSet(nil)
	eligible := newResourceRefSet(nil)
	held := map[string]heldDependency{}
	for _, item := range rendered {
		ref, err := templatesv1.ResourceRefFromObject(item.Object)
		if err != nil {
			continue
		}
		desired.insert(ref)
		if _, hadPrevious := existingEntries.get(ref); hadPrevious || len(item.Requires) == 0 {
			eligible.insert(ref)
			continue
		}
		blocker, ready, err := r.dependencyReady(ctx, clients, item, rendered)
		if err != nil {
			return nil, err
		}
		if ready {
			eligible.insert(ref)
			continue
		}
		held[resourceRefKey(ref)] = heldDependency{held: ref.ID, blocker: blocker}
	}
	if stats := statsFrom(ctx); stats != nil {
		for _, item := range held {
			stats.waiting = append(stats.waiting, item)
		}
		sort.Slice(stats.waiting, func(i, j int) bool { return stats.waiting[i].held < stats.waiting[j].held })
	}

	entries := newResourceRefSet(nil)
	appliedNew := 0
	stopped := false
	for _, item := range rendered {
		newResource := item.Object
		ref, err := templatesv1.ResourceRefFromObject(newResource)
		if err != nil {
			inventoryErr = errors.Join(inventoryErr, fmt.Errorf("failed to update inventory: %w", err))
			continue
		}
		if _, ok := held[resourceRefKey(ref)]; ok {
			continue
		}
		previous, hadPrevious := existingEntries.get(ref)
		if budget > 0 && !hadPrevious && appliedNew >= budget {
			stopped = true
			break
		}
		recordFailure := func(applyErr error) {
			if hadPrevious {
				failed := previous
				failed.LastError = applyErr.Error()
				entries.insert(failed)
			}
			inventoryErr = errors.Join(inventoryErr, applyErr)
		}

		hash, err := objectRenderHash(newResource)
		if err != nil {
			recordFailure(err)
			if budget > 0 {
				return rolloutResult(entries, existingEntries, desired, appliedNew, err)
			}
			continue
		}
		policy := resolvedDeletionPolicy(item.DeletionPolicy, gitOpsSet)
		account := resolvedServiceAccount(item.ServiceAccountName, clients.reconcileAccount)
		if hadPrevious && previous.RenderHash != "" && previous.RenderHash == hash && previous.LastError == "" {
			applyClient, err := clients.get(account)
			if err != nil {
				recordFailure(err)
				if budget > 0 {
					return rolloutResult(entries, existingEntries, desired, appliedNew, err)
				}
				continue
			}
			current := &unstructured.Unstructured{}
			current.SetGroupVersionKind(newResource.GroupVersionKind())
			if err := applyClient.Get(ctx, client.ObjectKeyFromObject(newResource), current); err != nil {
				if !apierrors.IsNotFound(err) {
					recordFailure(fmt.Errorf("failed to load existing Resource: %w", err))
					if budget > 0 {
						return rolloutResult(entries, existingEntries, desired, appliedNew, err)
					}
					continue
				}
			} else {
				updated := previous
				updated.DeletionPolicy = policy
				updated.ServiceAccountName = account
				entries.insert(updated)
				continue
			}
		}

		if err := logResourceMessage(logger, "applying resource", newResource); err != nil {
			recordFailure(err)
			if budget > 0 {
				return rolloutResult(entries, existingEntries, desired, appliedNew, err)
			}
			continue
		}
		applyClient, err := clients.get(account)
		if err != nil {
			recordFailure(err)
			if budget > 0 {
				return rolloutResult(entries, existingEntries, desired, appliedNew, err)
			}
			continue
		}
		if err := r.applyResource(ctx, applyClient, gitOpsSet, newResource); err != nil {
			if missingNamespace(err) {
				retainUnreached(entries, existingEntries, desired)
				return &templatesv1.ResourceInventory{Entries: entries.list()}, &retryAfterError{after: 5 * time.Second, err: err}
			}
			recordFailure(err)
			if budget > 0 {
				return rolloutResult(entries, existingEntries, desired, appliedNew, err)
			}
			continue
		}
		revision := ""
		if hadPrevious {
			revision = previous.SourceRevision
		}
		applied := appliedResourceRef(ref, revision)
		applied.RenderHash = hash
		applied.DeletionPolicy = policy
		applied.ServiceAccountName = account
		entries.insert(applied)
		notePatched(ctx)
		if !hadPrevious {
			appliedNew++
		}
	}
	if budget > 0 {
		retainUnreached(entries, existingEntries, desired)
	}

	if gitOpsSet.Status.Inventory != nil {
		removed := existingEntries.difference(entries)
		if budget > 0 {
			removed = existingEntries.difference(desired)
		}
		var deleting []templatesv1.ResourceRef
		for _, ref := range removed {
			if entryOrphans(ref, gitOpsSet) {
				continue
			}
			deleting = append(deleting, ref)
		}
		failedDeletes, err := r.removeResourceRefs(ctx, clients, deleting)
		if err != nil {
			inventoryErr = errors.Join(inventoryErr, err)
		}
		for _, failed := range failedDeletes {
			entries.insert(failed)
		}
	}
	if progress := progressAfterStop(stopped, entries, eligible, appliedNew); progress != nil {
		return &templatesv1.ResourceInventory{Entries: entries.list()}, progress
	}

	return &templatesv1.ResourceInventory{Entries: entries.list()}, inventoryErr
}

func progressAfterStop(stopped bool, entries, desired *resourceRefSet, applied int) *rolloutProgressError {
	if !stopped {
		return nil
	}
	return newRolloutProgress(entries, desired, applied)
}

func rolloutBudget(gitOpsSet *templatesv1.GitOpsSet) int {
	if gitOpsSet.Spec.Rollout == nil || gitOpsSet.Spec.Rollout.MaxResources == nil || *gitOpsSet.Spec.Rollout.MaxResources <= 0 {
		return 0
	}
	return *gitOpsSet.Spec.Rollout.MaxResources
}

func retainUnreached(entries, existing, desired *resourceRefSet) {
	for _, prev := range existing.list() {
		if _, wanted := desired.get(prev); !wanted {
			continue
		}
		if _, present := entries.get(prev); present {
			continue
		}
		entries.insert(prev)
	}
}

func rolloutResult(entries, existing, desired *resourceRefSet, applied int, err error) (*templatesv1.ResourceInventory, error) {
	retainUnreached(entries, existing, desired)
	return &templatesv1.ResourceInventory{Entries: entries.list()}, err
}

func newRolloutProgress(entries, desired *resourceRefSet, applied int) *rolloutProgressError {
	remaining := 0
	for _, ref := range desired.list() {
		if _, present := entries.get(ref); !present {
			remaining++
		}
	}
	if remaining == 0 {
		return nil
	}
	return &rolloutProgressError{applied: applied, remaining: remaining}
}

type rolloutProgressError struct {
	applied   int
	remaining int
}

func (e *rolloutProgressError) Error() string {
	return fmt.Sprintf("rollout applied %d resources, %d remaining", e.applied, e.remaining)
}

type retryAfterError struct {
	after time.Duration
	err   error
}

func (e *retryAfterError) Error() string { return e.err.Error() }

func (e *retryAfterError) Unwrap() error { return e.err }

func missingNamespace(err error) bool {
	return apierrors.IsNotFound(err) && strings.Contains(err.Error(), "namespaces ")
}

func orderResourcesForApply(mapper meta.RESTMapper, resources []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	ordered := append([]*unstructured.Unstructured(nil), resources...)
	var scopeErr error
	sort.SliceStable(ordered, func(i, j int) bool {
		return applyRank(mapper, ordered[i], &scopeErr) < applyRank(mapper, ordered[j], &scopeErr)
	})
	return ordered, scopeErr
}

func applyRank(mapper meta.RESTMapper, obj *unstructured.Unstructured, scopeErr *error) int {
	namespaced, err := templates.ObjectIsNamespaced(mapper, obj)
	if err != nil {
		*scopeErr = err
		return 2
	}
	if !namespaced && obj.GetKind() == "Namespace" {
		return 0
	}
	if !namespaced {
		return 1
	}
	return 2
}

type applyStatsKeyType struct{}

var applyStatsKey applyStatsKeyType

type heldDependency struct {
	held    string
	blocker string
}

type applyStats struct {
	patched bool
	waiting []heldDependency
}

func statsFrom(ctx context.Context) *applyStats {
	stats, _ := ctx.Value(applyStatsKey).(*applyStats)
	return stats
}

func notePatched(ctx context.Context) {
	if stats := statsFrom(ctx); stats != nil {
		stats.patched = true
	}
}

func waitingMessage(waiting []heldDependency) string {
	const maxShown = 5
	parts := make([]string, 0, len(waiting))
	for _, item := range waiting {
		parts = append(parts, item.held+" is waiting for "+item.blocker)
	}
	shown := parts
	extra := ""
	if len(parts) > maxShown {
		shown = parts[:maxShown]
		extra = fmt.Sprintf(", and %d more", len(parts)-maxShown)
	}
	return fmt.Sprintf("%d resources are waiting for a dependency: %s%s", len(parts), strings.Join(shown, ", "), extra)
}

func objectRenderHash(obj *unstructured.Unstructured) (string, error) {
	raw, err := json.Marshal(obj.Object)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func resolvedDeletionPolicy(templatePolicy string, gitOpsSet *templatesv1.GitOpsSet) string {
	if templatePolicy != "" {
		return templatePolicy
	}
	if gitOpsSet.Spec.DeletionPolicy == templatesv1.DeletionPolicyOrphan {
		return templatesv1.DeletionPolicyOrphan
	}
	return templatesv1.DeletionPolicyDelete
}

func resolvedServiceAccount(templateAccount, reconcileAccount string) string {
	if templateAccount != "" {
		return templateAccount
	}
	return reconcileAccount
}

func entryOrphans(ref templatesv1.ResourceRef, gitOpsSet *templatesv1.GitOpsSet) bool {
	switch ref.DeletionPolicy {
	case templatesv1.DeletionPolicyOrphan:
		return true
	case templatesv1.DeletionPolicyDelete:
		return false
	default:
		return orphansResources(gitOpsSet)
	}
}

type clientSet struct {
	fallback         client.Client
	reconcileAccount string
	namespace        string
	cache            map[string]client.Client
	r                *GitOpsSetReconciler
}

func newClientSet(r *GitOpsSetReconciler, gitOpsSet *templatesv1.GitOpsSet, fallback client.Client) *clientSet {
	account := gitOpsSet.Spec.ServiceAccountName
	if account == "" {
		account = r.DefaultServiceAccount
	}
	return &clientSet{
		fallback:         fallback,
		reconcileAccount: account,
		namespace:        gitOpsSet.Namespace,
		cache:            map[string]client.Client{},
		r:                r,
	}
}

func (s *clientSet) get(account string) (client.Client, error) {
	if account == "" || account == s.reconcileAccount {
		return s.fallback, nil
	}
	if existing, ok := s.cache[account]; ok {
		return existing, nil
	}
	var (
		next client.Client
		err  error
	)
	if s.r.impersonationClient != nil {
		next, err = s.r.impersonationClient(s.namespace, account)
	} else {
		next, err = s.r.makeImpersonationClient(s.namespace, account)
	}
	if err != nil {
		return nil, err
	}
	s.cache[account] = next
	return next, nil
}

func (r *GitOpsSetReconciler) dependencyReady(ctx context.Context, clients *clientSet, item templates.RenderedObject, all []templates.RenderedObject) (string, bool, error) {
	for _, name := range item.Requires {
		found := false
		for _, other := range all {
			if other.ElementKey != item.ElementKey || other.TemplateName != name {
				continue
			}
			found = true
			ref, err := templatesv1.ResourceRefFromObject(other.Object)
			if err != nil {
				return name, false, nil
			}
			account := resolvedServiceAccount(other.ServiceAccountName, clients.reconcileAccount)
			reader, err := clients.get(account)
			if err != nil {
				return "", false, err
			}
			live := other.Object.DeepCopy()
			if err := reader.Get(ctx, client.ObjectKeyFromObject(live), live); err != nil {
				return ref.ID, false, nil
			}
			if !objectIsHealthy(live) {
				return ref.ID, false, nil
			}
		}
		if !found {
			return name, false, nil
		}
	}
	return "", true, nil
}

func orderRendered(mapper meta.RESTMapper, rendered []templates.RenderedObject) ([]templates.RenderedObject, error) {
	ordered := append([]templates.RenderedObject(nil), rendered...)
	var scopeErr error
	sort.SliceStable(ordered, func(i, j int) bool {
		return applyRank(mapper, ordered[i].Object, &scopeErr) < applyRank(mapper, ordered[j].Object, &scopeErr)
	})
	return ordered, scopeErr
}

func appliedResourceRef(ref templatesv1.ResourceRef, sourceRevision string) templatesv1.ResourceRef {
	now := metav1.Now()
	ref.LastAppliedTime = &now
	ref.LastError = ""
	ref.SourceRevision = sourceRevision
	return ref
}

type resourceRefSet struct {
	items map[string]templatesv1.ResourceRef
}

func newResourceRefSet(entries []templatesv1.ResourceRef) *resourceRefSet {
	set := &resourceRefSet{items: map[string]templatesv1.ResourceRef{}}
	for _, entry := range entries {
		set.insert(entry)
	}
	return set
}

func resourceRefKey(ref templatesv1.ResourceRef) string {
	return ref.ID + "\x00" + ref.Version
}

func (s *resourceRefSet) insert(ref templatesv1.ResourceRef) {
	s.items[resourceRefKey(ref)] = ref
}

func (s *resourceRefSet) get(ref templatesv1.ResourceRef) (templatesv1.ResourceRef, bool) {
	got, ok := s.items[resourceRefKey(ref)]
	return got, ok
}

func (s *resourceRefSet) difference(other *resourceRefSet) []templatesv1.ResourceRef {
	var gone []templatesv1.ResourceRef
	for key, ref := range s.items {
		if _, ok := other.items[key]; !ok {
			gone = append(gone, ref)
		}
	}
	return gone
}

func (s *resourceRefSet) list() []templatesv1.ResourceRef {
	refs := make([]templatesv1.ResourceRef, 0, len(s.items))
	for _, ref := range s.items {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ID == refs[j].ID {
			return refs[i].Version < refs[j].Version
		}
		return refs[i].ID < refs[j].ID
	})
	return refs
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

func (r *GitOpsSetReconciler) removeResourceRefs(ctx context.Context, clients *clientSet, deletions []templatesv1.ResourceRef) ([]templatesv1.ResourceRef, error) {
	logger := log.FromContext(ctx)
	var failed []templatesv1.ResourceRef
	var deleteErr error
	for _, v := range deletions {
		u, err := unstructuredFromResourceRef(v)
		if err != nil {
			v.LastError = err.Error()
			failed = append(failed, v)
			deleteErr = errors.Join(deleteErr, err)
			continue
		}
		if err := logResourceMessage(logger, "deleting resource", u); err != nil {
			v.LastError = err.Error()
			failed = append(failed, v)
			deleteErr = errors.Join(deleteErr, err)
			continue
		}

		k8sClient, err := clients.get(v.ServiceAccountName)
		if err != nil {
			v.LastError = err.Error()
			failed = append(failed, v)
			deleteErr = errors.Join(deleteErr, err)
			continue
		}
		if err := k8sClient.Delete(ctx, u); err != nil && !apierrors.IsNotFound(err) {
			deleteErr = errors.Join(deleteErr, fmt.Errorf("failed to delete %s: %w", v.ID, err))
			v.LastError = err.Error()
			failed = append(failed, v)
		}
	}

	return failed, deleteErr
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitOpsSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Index the GitOpsSets by the GitRepository references they (may) point at.
	if err := mgr.GetCache().IndexField(
		context.TODO(), &templatesv1.GitOpsSet{}, gitRepositoryIndexKey, indexGitRepositories); err != nil {
		return fmt.Errorf("failed setting index field for GitRepository: %w", err)
	}

	if err := mgr.GetCache().IndexField(
		context.TODO(), &templatesv1.GitOpsSet{}, configMapIndexKey, indexConfig("ConfigMap")); err != nil {
		return fmt.Errorf("failed setting index field for ConfigMap: %w", err)
	}

	if err := mgr.GetCache().IndexField(
		context.TODO(), &templatesv1.GitOpsSet{}, secretIndexKey, indexConfig("Secret")); err != nil {
		return fmt.Errorf("failed setting index field for Secret: %w", err)
	}

	if err := mgr.GetCache().IndexField(
		context.TODO(), &templatesv1.GitOpsSet{}, ociRepositoryIndexKey, indexOCIRepositories); err != nil {
		return fmt.Errorf("failed setting index field for OCIRepository: %w", err)
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&templatesv1.GitOpsSet{}, builder.WithPredicates(
			predicate.Or(predicate.GenerationChangedPredicate{}, predicates.ReconcileRequestedPredicate{}))).
		Watches(
			&sourcev1.GitRepository{},
			handler.EnqueueRequestsFromMapFunc(r.gitRepositoryToGitOpsSet),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.secretToGitOpsSet),
		)

	if r.Generators["Config"] != nil {
		builder.Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.configMapToGitOpsSet),
		)
	}

	if r.Generators["OCIRepository"] != nil {
		builder.Watches(
			&sourcev1beta2.OCIRepository{},
			handler.EnqueueRequestsFromMapFunc(r.ociRepositoryToGitOpsSet),
		)
	}

	// Only watch for GitopsCluster objects if the Cluster generator is enabled.
	if r.Generators["Cluster"] != nil {
		if err := mgr.GetCache().IndexField(
			context.TODO(), &templatesv1.GitOpsSet{}, clusterGeneratorIndexKey, indexClusterGenerators); err != nil {
			return fmt.Errorf("failed setting index field for Cluster generator: %w", err)
		}
		builder.Watches(
			&clustersv1.GitopsCluster{},
			handler.EnqueueRequestsFromMapFunc(r.gitOpsClusterToGitOpsSet),
		)
	}

	// Only watch for ImagePolicy objects if the ImagePolicy generator is enabled.
	if r.Generators["ImagePolicy"] != nil {
		// Index the GitOpsSets by the ImageRepository references they (may) point at.
		if err := mgr.GetCache().IndexField(
			context.TODO(), &templatesv1.GitOpsSet{}, imagePolicyIndexKey, indexImagePolicies); err != nil {
			return fmt.Errorf("failed setting index fields: %w", err)
		}

		builder.Watches(
			&imagev1.ImagePolicy{},
			handler.EnqueueRequestsFromMapFunc(r.imagePolicyToGitOpsSet),
		)
	}

	return builder.Complete(r)
}

// gitOpsClusterToGitOpsSet maps a GitopsCluster object to its related GitOpsSet objects
// and returns a list of reconcile requests for the GitOpsSets.
func (r *GitOpsSetReconciler) gitOpsClusterToGitOpsSet(ctx context.Context, o client.Object) []reconcile.Request {
	gitOpsCluster, ok := o.(*clustersv1.GitopsCluster)
	if !ok {
		return nil
	}

	list := &templatesv1.GitOpsSetList{}

	err := r.List(ctx, list, client.MatchingFields{clusterGeneratorIndexKey: "true"})
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to list GitOpsSets for GitopsCluster")
		return nil
	}

	var result []reconcile.Request
	for _, v := range list.Items {
		if matchCluster(gitOpsCluster, &v) {
			result = append(result, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&v)})
		}
	}

	return result
}

func (r *GitOpsSetReconciler) finalize(ctx context.Context, gs *templatesv1.GitOpsSet, k8sClient client.Client) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("finalizing resources")

	if gs.Status.Inventory != nil && gs.Status.Inventory.Entries != nil {
		clients := newClientSet(r, gs, k8sClient)
		var deleting []templatesv1.ResourceRef
		for _, entry := range gs.Status.Inventory.Entries {
			if entryOrphans(entry, gs) {
				continue
			}
			deleting = append(deleting, entry)
		}
		if _, err := r.removeResourceRefs(ctx, clients, deleting); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("cleaned resources")
	}

	logger.Info("removing the finalizer")
	// Remove our finalizer from the list and update it
	controllerutil.RemoveFinalizer(gs, templatesv1.GitOpsSetFinalizer)
	return ctrl.Result{}, r.Update(ctx, gs)
}

func orphansResources(gs *templatesv1.GitOpsSet) bool {
	return gs.Spec.DeletionPolicy == templatesv1.DeletionPolicyOrphan
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

func (r *GitOpsSetReconciler) gitRepositoryToGitOpsSet(ctx context.Context, obj client.Object) []reconcile.Request {
	// TODO: Store the applied version of GitRepositories in the Status, and don't
	// retrigger if the commit-id isn't different.
	return r.queryIndexedGitOpsSets(ctx, gitRepositoryIndexKey, obj)
}

func (r *GitOpsSetReconciler) ociRepositoryToGitOpsSet(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.queryIndexedGitOpsSets(ctx, ociRepositoryIndexKey, obj)
}

func (r *GitOpsSetReconciler) imagePolicyToGitOpsSet(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.queryIndexedGitOpsSets(ctx, imagePolicyIndexKey, obj)
}

func (r *GitOpsSetReconciler) queryIndexedGitOpsSets(ctx context.Context, key string, obj client.Object) []reconcile.Request {
	var list templatesv1.GitOpsSetList

	if err := r.List(ctx, &list,
		client.MatchingFields{key: client.ObjectKeyFromObject(obj).String()},
		client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}

	result := []reconcile.Request{}
	for i := range list.Items {
		result = append(result, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}

	return result
}

func (r *GitOpsSetReconciler) configMapToGitOpsSet(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.queryIndexedGitOpsSets(ctx, configMapIndexKey, obj)
}

func (r *GitOpsSetReconciler) secretToGitOpsSet(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.queryIndexedGitOpsSets(ctx, secretIndexKey, obj)
}

func (r *GitOpsSetReconciler) makeImpersonationClient(namespace, serviceAccountName string) (client.Client, error) {
	copyCfg := rest.CopyConfig(r.Config)

	copyCfg.Impersonate = rest.ImpersonationConfig{
		UserName: fmt.Sprintf("system:serviceaccount:%s:%s", namespace, serviceAccountName),
	}

	return client.New(copyCfg, client.Options{Scheme: r.Scheme, Mapper: r.Mapper})
}

func indexClusterGenerators(o client.Object) []string {
	ks, ok := o.(*templatesv1.GitOpsSet)
	if !ok {
		panic(fmt.Sprintf("Expected a GitOpsSet, got %T", o))
	}
	for _, generator := range ks.Spec.Generators {
		if len(getClusterSelectors(generator)) > 0 {
			return []string{"true"}
		}
	}
	return nil
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
		if gen.Matrix != nil && gen.Matrix.Generators != nil {
			for _, matrixGen := range gen.Matrix.Generators {
				if matrixGen.GitRepository != nil {
					referencedRepositories = append(referencedRepositories, matrixGen.GitRepository)
				}
			}
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

func indexOCIRepositories(o client.Object) []string {
	ks, ok := o.(*templatesv1.GitOpsSet)
	if !ok {
		panic(fmt.Sprintf("Expected a GitOpsSet, got %T", o))
	}

	referencedRepositories := []*templatesv1.OCIRepositoryGenerator{}
	for _, gen := range ks.Spec.Generators {
		if gen.OCIRepository != nil {
			referencedRepositories = append(referencedRepositories, gen.OCIRepository)
		}
		if gen.Matrix != nil && gen.Matrix.Generators != nil {
			for _, matrixGen := range gen.Matrix.Generators {
				if matrixGen.OCIRepository != nil {
					referencedRepositories = append(referencedRepositories, matrixGen.OCIRepository)
				}
			}
		}
	}

	if len(referencedRepositories) == 0 {
		return nil
	}

	referencedNames := []string{}
	for _, org := range referencedRepositories {
		referencedNames = append(referencedNames, fmt.Sprintf("%s/%s", ks.GetNamespace(), org.RepositoryRef))
	}

	return referencedNames
}

func indexConfig(kind string) func(o client.Object) []string {
	return func(o client.Object) []string {
		ks, ok := o.(*templatesv1.GitOpsSet)
		if !ok {
			panic(fmt.Sprintf("Expected a GitOpsSet, got %T", o))
		}

		return referencedConfigNames(ks, kind)
	}
}

func referencedConfigNames(ks *templatesv1.GitOpsSet, kind string) []string {
	var names []string
	add := func(name string) {
		if name == "" {
			return
		}
		names = append(names, fmt.Sprintf("%s/%s", ks.GetNamespace(), name))
	}
	considerConfig := func(cfg *templatesv1.ConfigGenerator) {
		if cfg != nil && cfg.Kind == kind {
			add(cfg.Name)
		}
	}
	considerPullRequest := func(pr *templatesv1.PullRequestGenerator) {
		if kind == "Secret" && pr != nil && pr.SecretRef != nil {
			add(pr.SecretRef.Name)
		}
	}
	considerAPI := func(ac *templatesv1.APIClientGenerator) {
		if ac == nil {
			return
		}
		if kind == "Secret" && ac.SecretRef != nil {
			add(ac.SecretRef.Name)
		}
		if ac.HeadersRef != nil && ac.HeadersRef.Kind == kind {
			add(ac.HeadersRef.Name)
		}
	}
	for _, gen := range ks.Spec.Generators {
		considerConfig(gen.Config)
		considerPullRequest(gen.PullRequests)
		considerAPI(gen.APIClient)
		if gen.Matrix == nil {
			continue
		}
		for _, nested := range gen.Matrix.Generators {
			considerConfig(nested.Config)
			considerPullRequest(nested.PullRequests)
			considerAPI(nested.APIClient)
		}
	}
	return names
}

func indexImagePolicies(o client.Object) []string {
	ks, ok := o.(*templatesv1.GitOpsSet)
	if !ok {
		panic(fmt.Sprintf("Expected a GitOpsSet, got %T", o))
	}

	referencedPolicies := []*templatesv1.ImagePolicyGenerator{}
	for _, gen := range ks.Spec.Generators {
		if gen.ImagePolicy != nil {
			referencedPolicies = append(referencedPolicies, gen.ImagePolicy)
			continue
		}
		if gen.Matrix != nil && gen.Matrix.Generators != nil {
			for _, matrixGen := range gen.Matrix.Generators {
				if matrixGen.ImagePolicy != nil {
					referencedPolicies = append(referencedPolicies, matrixGen.ImagePolicy)
				}
			}
		}
	}

	if len(referencedPolicies) == 0 {
		return nil
	}

	referencedNames := []string{}
	for _, ip := range referencedPolicies {
		referencedNames = append(referencedNames, fmt.Sprintf("%s/%s", ks.GetNamespace(), ip.PolicyRef))
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

const (
	gitOpsSetNameLabel      = "sets.gitops.pro/name"
	gitOpsSetNamespaceLabel = "sets.gitops.pro/namespace"
	gitOpsSetFieldManager   = "gitopssets-controller"
)

func (r *GitOpsSetReconciler) applyResource(ctx context.Context, k8sClient client.Client, gitOpsSet *templatesv1.GitOpsSet, newResource *unstructured.Unstructured) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(newResource.GroupVersionKind())
	err := k8sClient.Get(ctx, client.ObjectKeyFromObject(newResource), existing)
	if err == nil {
		if owner, namespace, ok := ownedByGitOpsSet(existing.GetLabels()); ok {
			if owner != gitOpsSet.GetName() || namespace != gitOpsSet.GetNamespace() {
				ref, refErr := templatesv1.ResourceRefFromObject(newResource)
				if refErr != nil {
					return fmt.Errorf("resource is owned by GitOpsSet %s/%s", namespace, owner)
				}
				return fmt.Errorf("%s is owned by GitOpsSet %s/%s", ref.ID, namespace, owner)
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to load existing Resource: %w", err)
	}

	applyObj := newResource.DeepCopy()
	unstructured.RemoveNestedField(applyObj.Object, "status")
	applyObj.SetResourceVersion("")
	applyObj.SetManagedFields(nil)
	applyObj.SetUID("")
	applyObj.SetGeneration(0)

	options := []client.PatchOption{&client.PatchOptions{FieldManager: gitOpsSetFieldManager}}
	if gitOpsSet.Spec.Force {
		options = append(options, client.ForceOwnership)
	}
	if err := k8sClient.Patch(ctx, applyObj, client.Apply, options...); err != nil {
		return fmt.Errorf("failed to apply Resource: %w", err)
	}
	return nil
}

func ownedByGitOpsSet(labels map[string]string) (name, namespace string, ok bool) {
	if len(labels) == 0 {
		return "", "", false
	}
	name, nameOK := labels[gitOpsSetNameLabel]
	namespace, namespaceOK := labels[gitOpsSetNamespaceLabel]
	if !nameOK && !namespaceOK {
		return "", "", false
	}
	return name, namespace, true
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

func calculateInterval(gs *templatesv1.GitOpsSet, configuredGenerators map[string]generators.Generator) (time.Duration, error) {
	res := []time.Duration{}
	for _, mg := range gs.Spec.Generators {
		relevantGenerators, err := generators.FindRelevantGenerators(mg, configuredGenerators)
		if err != nil {
			return generators.NoRequeueInterval, err
		}

		for _, rg := range relevantGenerators {
			d := rg.Interval(&mg)

			if d > generators.NoRequeueInterval {
				res = append(res, d)
			}

		}
	}

	if len(res) == 0 {
		return generators.NoRequeueInterval, nil
	}

	// Find the lowest requeue interval provided by a generator.
	sort.Slice(res, func(i, j int) bool { return res[i] < res[j] })

	return res[0], nil
}
