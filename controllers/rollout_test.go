package controllers

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators/list"
)

func TestRolloutAppliesNewObjectsInBatches(t *testing.T) {
	cl, gens := rolloutClient(t)
	maxResources := 2
	gs := gitOpsSetRendering("a", "b", "c", "d", "e")
	gs.Spec.Rollout = &templatesv1.Rollout{MaxResources: &maxResources}
	reconciler := &GitOpsSetReconciler{}

	inventory, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	assertRollout(t, err, 2, 3)
	if len(inventory.Entries) != 2 {
		t.Fatalf("entries = %d", len(inventory.Entries))
	}

	gs = gitOpsSetWithInventory(gs, inventory)
	inventory, err = reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	assertRollout(t, err, 2, 1)
	if len(inventory.Entries) != 4 {
		t.Fatalf("entries = %d", len(inventory.Entries))
	}

	gs = gitOpsSetWithInventory(gs, inventory)
	inventory, err = reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Entries) != 5 {
		t.Fatalf("entries = %d", len(inventory.Entries))
	}
}

func TestRolloutAppliesNamespaceBeforeDependents(t *testing.T) {
	cl, gens := rolloutClient(t)
	maxResources := 1
	gs := gitOpsSetRendering("preview")
	gs.Spec.Rollout = &templatesv1.Rollout{MaxResources: &maxResources}
	gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
		{Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"app","namespace":"preview"},"data":{"ok":"yes"}}`)}},
		{Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"preview"}}`)}},
	}

	inventory, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	assertRollout(t, err, 1, 1)
	if len(inventory.Entries) != 1 || inventory.Entries[0].ID == "" {
		t.Fatalf("inventory = %#v", inventory.Entries)
	}
	if err := cl.Get(t.Context(), types.NamespacedName{Name: "preview"}, &corev1.Namespace{}); err != nil {
		t.Fatalf("namespace: %v", err)
	}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "preview", Name: "app"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("configmap get = %v, want NotFound", err)
	}
}

func TestRolloutStopsAfterAFailedApply(t *testing.T) {
	base, gens := rolloutClient(t)
	cl := &failNthPatchClient{Client: base, failOn: 2}
	maxResources := 5
	gs := gitOpsSetRendering("a", "b", "c")
	gs.Spec.Rollout = &templatesv1.Rollout{MaxResources: &maxResources}

	inventory, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err == nil || errors.As(err, new(*rolloutProgressError)) {
		t.Fatalf("err = %v", err)
	}
	if len(inventory.Entries) != 1 {
		t.Fatalf("entries = %#v", inventory.Entries)
	}
	if err := base.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "c"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("later configmap get = %v", err)
	}
}

func TestRolloutOrphanDropsInventoryWithoutDeleting(t *testing.T) {
	cl, gens := rolloutClient(t)
	kept := newConfigMap("demo", "kept")
	dropped := newConfigMap("demo", "dropped")
	keptRef := mustResourceRef(t, kept)
	droppedRef := mustResourceRef(t, dropped)
	if err := cl.Create(t.Context(), kept); err != nil {
		t.Fatal(err)
	}
	if err := cl.Create(t.Context(), dropped); err != nil {
		t.Fatal(err)
	}
	maxResources := 1
	gs := gitOpsSetRendering("kept")
	gs.Spec.DeletionPolicy = templatesv1.DeletionPolicyOrphan
	gs.Spec.Rollout = &templatesv1.Rollout{MaxResources: &maxResources}
	gs.Status.Inventory = &templatesv1.ResourceInventory{Entries: []templatesv1.ResourceRef{keptRef, droppedRef}}

	inventory, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Entries) != 1 || inventory.Entries[0].ID != keptRef.ID {
		t.Fatalf("inventory = %#v, want %s", inventory.Entries, keptRef.ID)
	}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "dropped"}, &corev1.ConfigMap{}); err != nil {
		t.Fatalf("orphaned configmap: %v", err)
	}
}

func assertRollout(t *testing.T, err error, applied, remaining int) {
	t.Helper()
	var progress *rolloutProgressError
	if !errors.As(err, &progress) || progress.applied != applied || progress.remaining != remaining {
		t.Fatalf("err = %v, want applied %d remaining %d", err, applied, remaining)
	}
}

func rolloutClient(t *testing.T) (client.Client, map[string]generators.Generator) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).Build(), map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	}
}

type failNthPatchClient struct {
	client.Client
	failOn int
	calls  int
}

func (c *failNthPatchClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.calls++
	if c.calls == c.failOn {
		return fmt.Errorf("admission denied")
	}
	return c.Client.Patch(ctx, obj, patch, opts...)
}
