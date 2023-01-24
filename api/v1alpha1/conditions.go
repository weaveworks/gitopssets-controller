package v1alpha1

import (
	"github.com/fluxcd/pkg/apis/meta"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ReconciliationFailedReason represents the fact that
	// the reconciliation failed.
	ReconciliationFailedReason string = "ReconciliationFailed"

	// ReconciliationSucceededReason represents the fact that
	// the reconciliation succeeded.
	ReconciliationSucceededReason string = "ReconciliationSucceeded"
)

// SetGitOpsSetReadiness sets the ready condition with the given status, reason and message.
func SetGitOpsSetReadiness(set *GitOpsSet, status metav1.ConditionStatus, reason, message string) {
	set.Status.ObservedGeneration = set.ObjectMeta.Generation
	newCondition := metav1.Condition{
		Type:    meta.ReadyCondition,
		Status:  status,
		Reason:  reason,
		Message: message,
	}
	apimeta.SetStatusCondition(&set.Status.Conditions, newCondition)
}

// SetReadyWithInventory updates the GitOpsSet to reflect the new readiness and
// store the current inventory.
func SetReadyWithInventory(set *GitOpsSet, inventory *ResourceInventory, reason, message string) {
	set.Status.Inventory = inventory

	if len(inventory.Entries) == 0 {
		set.Status.Inventory = nil
	}

	SetGitOpsSetReadiness(set, metav1.ConditionTrue, reason, message)
}
