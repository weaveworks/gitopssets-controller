package controllers

import (
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestSuspendedGitOpsSetDoesNotPrune(t *testing.T) {
	scheme := newSuspendScheme(t)
	cm := newConfigMap("demo", "owned")
	gs := suspendedGitOpsSet(cm)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&templatesv1.GitOpsSet{}).WithRuntimeObjects(gs, cm).Build()
	reconciler := &GitOpsSetReconciler{Client: cl}

	if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)}); err != nil {
		t.Fatal(err)
	}

	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "owned"}, &corev1.ConfigMap{}); err != nil {
		t.Fatalf("owned configmap was deleted while suspended: %v", err)
	}
	updated := &templatesv1.GitOpsSet{}
	if err := cl.Get(t.Context(), client.ObjectKeyFromObject(gs), updated); err != nil {
		t.Fatal(err)
	}
	condition := apimeta.FindStatusCondition(updated.Status.Conditions, "Ready")
	if condition == nil || condition.Reason != templatesv1.SuspendedReason || condition.Status != metav1.ConditionFalse {
		t.Fatalf("ready condition = %#v, want False/%s", condition, templatesv1.SuspendedReason)
	}
	if updated.Status.Inventory == nil || len(updated.Status.Inventory.Entries) != 1 {
		t.Fatalf("inventory = %#v, want the owned configmap to remain", updated.Status.Inventory)
	}
}

func TestFinalizeDeletesInventoryWhenSuspended(t *testing.T) {
	scheme := newSuspendScheme(t)
	cm := newConfigMap("demo", "owned")
	gs := suspendedGitOpsSet(cm)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(gs, cm).Build()
	reconciler := &GitOpsSetReconciler{Client: cl}

	stored := &templatesv1.GitOpsSet{}
	if err := cl.Get(t.Context(), client.ObjectKeyFromObject(gs), stored); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.finalize(t.Context(), stored, cl); err != nil {
		t.Fatal(err)
	}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "owned"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("owned configmap get = %v, want NotFound", err)
	}
	if err := cl.Get(t.Context(), client.ObjectKeyFromObject(gs), stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Finalizers) != 0 {
		t.Fatalf("finalizers = %v, want none", stored.Finalizers)
	}
}

func newSuspendScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func suspendedGitOpsSet(cm *corev1.ConfigMap) *templatesv1.GitOpsSet {
	ref, err := templatesv1.ResourceRefFromObject(cm)
	if err != nil {
		panic(err)
	}
	return &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "set",
			Namespace:  "demo",
			Finalizers: []string{templatesv1.GitOpsSetFinalizer},
		},
		Spec: templatesv1.GitOpsSetSpec{Suspend: true},
		Status: templatesv1.GitOpsSetStatus{
			Inventory: &templatesv1.ResourceInventory{Entries: []templatesv1.ResourceRef{ref}},
		},
	}
}
