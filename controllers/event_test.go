package controllers

import (
	"strings"
	"testing"
	"time"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/test"
)

func TestEventIncludesReconcileError(t *testing.T) {
	recorder := &test.FakeEventRecorder{}
	reconciler := &GitOpsSetReconciler{EventRecorder: recorder}
	set := &templatesv1.GitOpsSet{ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "demo"}}
	templatesv1.SetGitOpsSetReadiness(set, nil, metav1.ConditionFalse, templatesv1.ReconciliationFailedReason, "configmaps \"app\" not found")

	reconciler.event(set, eventv1.EventSeverityError, "Reconciliation failed after %s: %s", time.Millisecond.String(), "configmaps \"app\" not found")

	if len(recorder.Events) != 1 {
		t.Fatalf("events = %d", len(recorder.Events))
	}
	got := recorder.Events[0]
	if got.EventType != "Warning" || got.Reason != templatesv1.ReconciliationFailedReason {
		t.Fatalf("event = %#v", got)
	}
	if !strings.Contains(got.Message, "configmaps \"app\" not found") {
		t.Fatalf("message = %q", got.Message)
	}
}

func TestTruncateEventMessage(t *testing.T) {
	short := truncateEventMessage("ok")
	if short != "ok" {
		t.Fatalf("short = %q", short)
	}
	long := truncateEventMessage(strings.Repeat("a", 2000))
	if len(long) != maxEventMessageLength || !strings.HasSuffix(long, "...(truncated)") {
		t.Fatalf("len=%d suffix=%v", len(long), strings.HasSuffix(long, "...(truncated)"))
	}
}
