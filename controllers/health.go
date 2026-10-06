package controllers

import (
	"context"
	"fmt"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

const healthRequeueInterval = 10 * time.Second

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

	var waiting []string
	for _, entry := range inventory.Entries {
		objMeta, err := object.ParseObjMetadata(entry.ID)
		if err != nil {
			continue
		}
		if _, ok := healthCheckKinds[objMeta.GroupKind.Kind]; !ok {
			continue
		}

		current := &unstructured.Unstructured{}
		current.SetGroupVersionKind(objMeta.GroupKind.WithVersion(entry.Version))
		current.SetName(objMeta.Name)
		current.SetNamespace(objMeta.Namespace)
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(current), current); err != nil {
			setHealthy(gitOpsSet, metav1.ConditionFalse, templatesv1.HealthFailedReason, fmt.Sprintf("failed to read %s: %s", entry.ID, err.Error()))
			return healthRequeueInterval
		}
		if !objectIsHealthy(current) {
			waiting = append(waiting, entry.ID)
		}
	}
	if len(waiting) > 0 {
		setHealthy(gitOpsSet, metav1.ConditionFalse, templatesv1.HealthProgressingReason, fmt.Sprintf("%d resources are not ready", len(waiting)))
		return healthRequeueInterval
	}
	setHealthy(gitOpsSet, metav1.ConditionTrue, templatesv1.HealthSucceededReason, "checked resources are ready")
	return 0
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
