package controllers

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators/list"
)

func TestRenderAndReconcileAdoptsUnownedResource(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := newConfigMap("demo", "kept")
	existing.Data = map[string]string{"ok": "old"}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(existing).Build()
	gs := gitOpsSetRendering("kept")
	reconciler := &GitOpsSetReconciler{}

	_, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	})
	if err == nil || !apierrors.IsConflict(err) {
		t.Fatalf("err = %v, want a field-manager conflict", err)
	}
	got := &corev1.ConfigMap{}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "kept"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Data["ok"] != "old" {
		t.Fatalf("data = %v, want the existing value until force is set", got.Data)
	}

	gs.Spec.Force = true
	inventory, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Entries) != 1 || inventory.Entries[0].ID != mustResourceRef(t, existing).ID {
		t.Fatalf("inventory = %#v", inventory)
	}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "kept"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Data["ok"] != "yes" {
		t.Fatalf("data = %v, want adopted template value", got.Data)
	}
	if got.Labels[gitOpsSetNameLabel] != "set" || got.Labels[gitOpsSetNamespaceLabel] != "demo" {
		t.Fatalf("labels = %v, want ownership labels", got.Labels)
	}
}

func TestRenderAndReconcileDoesNotAdoptAnotherGitOpsSet(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := newConfigMap("demo", "kept")
	existing.Data = map[string]string{"ok": "theirs"}
	existing.Labels = map[string]string{
		gitOpsSetNameLabel:      "other",
		gitOpsSetNamespaceLabel: "demo",
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(existing).Build()
	gs := gitOpsSetRendering("kept")
	reconciler := &GitOpsSetReconciler{}

	_, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	})
	if err == nil || !strings.Contains(err.Error(), "demo/other") {
		t.Fatalf("error = %v, want an ownership conflict naming demo/other", err)
	}
	got := &corev1.ConfigMap{}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "kept"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Data["ok"] != "theirs" {
		t.Fatalf("data = %v, want the other set's value to stay", got.Data)
	}
}
