package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

const (
	healthRequeueInterval = 10 * time.Second
	healthRecheckInterval = time.Minute
	healthMessageLimit    = 5
)

var healthCheckKinds = map[string]struct{}{
	"Kustomization": {},
	"HelmRelease":   {},
	"Deployment":    {},
	"DaemonSet":     {},
	"StatefulSet":   {},
}

func (r *GitOpsSetReconciler) observeHealth(ctx context.Context, k8sClient client.Client, gitOpsSet *templatesv1.GitOpsSet, inventory *templatesv1.ResourceInventory) time.Duration {
	if !gitOpsSet.HealthChecksEnabled() {
		setHealthy(gitOpsSet, metav1.ConditionTrue, templatesv1.HealthDisabledReason, "health checks are disabled")
		return 0
	}
	if inventory == nil {
		setHealthy(gitOpsSet, metav1.ConditionTrue, templatesv1.HealthSucceededReason, "no resources to check")
		return 0
	}

	kinds := healthKinds(gitOpsSet)
	clients := newClientSet(r, gitOpsSet, k8sClient)
	var waiting []string
	checked := 0
	for _, entry := range inventory.Entries {
		objMeta, err := object.ParseObjMetadata(entry.ID)
		if err != nil {
			continue
		}
		if _, ok := kinds[objMeta.GroupKind.Kind]; !ok {
			continue
		}
		checked++

		current := &unstructured.Unstructured{}
		current.SetGroupVersionKind(objMeta.GroupKind.WithVersion(entry.Version))
		current.SetName(objMeta.Name)
		current.SetNamespace(objMeta.Namespace)
		reader, err := clients.get(entry.ServiceAccountName)
		if err != nil {
			setHealthy(gitOpsSet, metav1.ConditionFalse, templatesv1.HealthFailedReason, fmt.Sprintf("failed to read %s: %s", entry.ID, err.Error()))
			return healthRequeueInterval
		}
		if err := reader.Get(ctx, client.ObjectKeyFromObject(current), current); err != nil {
			if apierrors.IsForbidden(err) {
				setHealthy(gitOpsSet, metav1.ConditionFalse, templatesv1.AccessDeniedReason, fmt.Sprintf("%s: service account cannot get object: %s", entry.ID, err.Error()))
				return healthRequeueInterval
			}
			setHealthy(gitOpsSet, metav1.ConditionFalse, templatesv1.HealthFailedReason, fmt.Sprintf("failed to read %s: %s", entry.ID, err.Error()))
			return healthRequeueInterval
		}
		if !objectIsHealthy(current) {
			waiting = append(waiting, entry.ID)
		}
	}
	if len(waiting) > 0 {
		setHealthy(gitOpsSet, metav1.ConditionFalse, templatesv1.HealthProgressingReason, healthWaitingMessage(waiting))
		return healthRequeueInterval
	}
	if checked == 0 {
		setHealthy(gitOpsSet, metav1.ConditionTrue, templatesv1.HealthSucceededReason, "checked resources are ready")
		return 0
	}
	setHealthy(gitOpsSet, metav1.ConditionTrue, templatesv1.HealthSucceededReason, "checked resources are ready")
	return healthRecheckInterval
}

func healthKinds(gitOpsSet *templatesv1.GitOpsSet) map[string]struct{} {
	if gitOpsSet.Spec.HealthCheck == nil || len(gitOpsSet.Spec.HealthCheck.Kinds) == 0 {
		return healthCheckKinds
	}
	kinds := make(map[string]struct{}, len(gitOpsSet.Spec.HealthCheck.Kinds))
	for _, kind := range gitOpsSet.Spec.HealthCheck.Kinds {
		kinds[kind] = struct{}{}
	}
	return kinds
}

func healthWaitingMessage(waiting []string) string {
	shown := waiting
	extra := ""
	if len(waiting) > healthMessageLimit {
		shown = waiting[:healthMessageLimit]
		extra = fmt.Sprintf(", and %d more", len(waiting)-healthMessageLimit)
	}
	return fmt.Sprintf("%d resources are not ready: %s%s", len(waiting), strings.Join(shown, ", "), extra)
}

func setHealthy(gitOpsSet *templatesv1.GitOpsSet, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&gitOpsSet.Status.Conditions, metav1.Condition{
		Type:    templatesv1.HealthyCondition,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

func objectIsHealthy(obj *unstructured.Unstructured) bool {
	switch obj.GetKind() {
	case "Deployment":
		return statusAtLeast(obj, "availableReplicas", "replicas")
	case "DaemonSet":
		ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "numberReady")
		desired, _, _ := unstructured.NestedInt64(obj.Object, "status", "desiredNumberScheduled")
		return desired > 0 && ready >= desired
	case "StatefulSet":
		return statusAtLeast(obj, "readyReplicas", "replicas")
	default:
		return readyConditionTrue(obj)
	}
}

func statusAtLeast(obj *unstructured.Unstructured, statusField, specField string) bool {
	ready, _, _ := unstructured.NestedInt64(obj.Object, "status", statusField)
	want, found, _ := unstructured.NestedInt64(obj.Object, "spec", specField)
	if !found {
		want = 1
	}
	return ready >= want
}

func readyConditionTrue(obj *unstructured.Unstructured) bool {
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if condition["type"] == "Ready" && condition["status"] == string(metav1.ConditionTrue) {
			return true
		}
	}
	return false
}
