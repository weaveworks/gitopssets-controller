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
func SetGitOpsSetReadiness(set *GitOpsSet, inventory *ResourceInventory, status metav1.ConditionStatus, reason, message string) {
	if inventory != nil {
		set.Status.Inventory = inventory

		if len(inventory.Entries) == 0 {
			set.Status.Inventory = nil
		}
	}

	set.Status.ObservedGeneration = set.ObjectMeta.Generation
	newCondition := metav1.Condition{
		Type:    meta.ReadyCondition,
		Status:  status,
		Reason:  reason,
		Message: message,
	}
	apimeta.SetStatusCondition(&set.Status.Conditions, newCondition)
}

// GetGitOpsSetReadiness returns the readiness condition of the GitOpsSet.
func GetGitOpsSetReadiness(set *GitOpsSet) metav1.ConditionStatus {
	return apimeta.FindStatusCondition(set.Status.Conditions, meta.ReadyCondition).Status
}
