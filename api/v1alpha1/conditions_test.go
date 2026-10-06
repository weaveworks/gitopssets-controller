package v1alpha1

import (
	"testing"

	"github.com/fluxcd/pkg/apis/meta"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestReadinessAndConditions(t *testing.T) {
	set := &GitOpsSet{ObjectMeta: metav1.ObjectMeta{Generation: 4}}
	if got := GetGitOpsSetReadiness(set); got != metav1.ConditionUnknown {
		t.Fatalf("readiness = %s, want Unknown", got)
	}

	inventory := &ResourceInventory{Entries: []ResourceRef{{ID: "demo_app__ConfigMap", Version: "v1"}}}
	SetGitOpsSetReadiness(set, inventory, metav1.ConditionTrue, ReconciliationSucceededReason, "1 resources created")
	if got := GetGitOpsSetReadiness(set); got != metav1.ConditionTrue {
		t.Fatalf("readiness = %s, want True", got)
	}
	if set.Status.ObservedGeneration != 4 {
		t.Fatalf("observed generation = %d", set.Status.ObservedGeneration)
	}
	if len(set.GetConditions()) != 1 || set.GetConditions()[0].Reason != ReconciliationSucceededReason {
		t.Fatalf("conditions = %#v", set.GetConditions())
	}

	SetGitOpsSetReadiness(set, &ResourceInventory{}, metav1.ConditionFalse, ReconciliationFailedReason, "empty")
	if set.Status.Inventory != nil {
		t.Fatalf("empty inventory was stored: %#v", set.Status.Inventory)
	}

	replacement := []metav1.Condition{{Type: meta.ReadyCondition, Status: metav1.ConditionFalse, Reason: SuspendedReason}}
	set.SetConditions(replacement)
	if len(set.GetConditions()) != 1 || set.GetConditions()[0].Reason != SuspendedReason {
		t.Fatalf("set conditions = %#v", set.GetConditions())
	}
}

func TestResourceRefFromObject(t *testing.T) {
	cm := &corev1.ConfigMap{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}, ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "demo"}}
	ref, err := ResourceRefFromObject(cm)
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "demo_app__ConfigMap" || ref.Version != "v1" {
		t.Fatalf("ref = %#v", ref)
	}

	_, err = ResourceRefFromObject(&runtime.Unknown{})
	if err == nil {
		t.Fatal("expected an error for an object without metadata")
	}
}

func TestSchemeRegistersGitOpsSet(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	gvks, _, err := scheme.ObjectKinds(&GitOpsSet{})
	if err != nil {
		t.Fatal(err)
	}
	if len(gvks) == 0 || gvks[0].Kind != "GitOpsSet" {
		t.Fatalf("kinds = %#v", gvks)
	}
}
