package v1alpha1

import (
	"github.com/fluxcd/pkg/apis/meta"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	maxConditionMessageLength = 20000

	// HealthyCondition indicates that the GitOpsSet has created all its
	// resources.
	HealthyCondition string = "Healthy"
)

// GitOpsSetReady registers a successful apply attempt of the given GitOps.
func GitOpsSetReady(k GitOpsSet, inventory *ResourceInventory, reason, message string) GitOpsSet {
	setGitOpsSetReadiness(&k, metav1.ConditionTrue, reason, message)
	k.Status.Inventory = inventory
	return k
}

func setGitOpsSetReadiness(k *GitOpsSet, status metav1.ConditionStatus, reason, message string) {
	newCondition := metav1.Condition{
		Type:    meta.ReadyCondition,
		Status:  status,
		Reason:  reason,
		Message: limitMessage(message),
	}
	apimeta.SetStatusCondition(&k.Status.Conditions, newCondition)
}

// chop a string and add an ellipsis to indicate that it's been chopped.
func limitMessage(s string) string {
	if len(s) <= maxConditionMessageLength {
		return s
	}

	return s[0:maxConditionMessageLength-3] + "..."
}
