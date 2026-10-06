package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators/list"
)

func TestReconcileResourcesPassesImpersonatedClientToGenerators(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	controllerClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	impersonated := fake.NewClientBuilder().WithScheme(scheme).Build()
	var seen client.Reader
	reconciler := &GitOpsSetReconciler{
		Client: controllerClient,
		Generators: map[string]generators.GeneratorFactory{
			"List": func(l logr.Logger, c client.Reader) generators.Generator {
				seen = c
				return list.NewGenerator(l)
			},
		},
	}

	if _, _, err := reconciler.reconcileResources(t.Context(), impersonated, gitOpsSetRendering()); err != nil {
		t.Fatal(err)
	}
	if seen != impersonated {
		t.Fatal("generators received the controller client instead of the impersonated client")
	}
}

func TestRenderAndReconcileKeepsFailedDeletesInInventory(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	kept := newConfigMap("demo", "kept")
	gone := newConfigMap("demo", "gone")
	stuck := newConfigMap("demo", "stuck")
	base := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(kept, gone, stuck).Build()
	cl := &deleteFailClient{Client: base, fail: map[string]error{
		"demo/stuck": fmt.Errorf("admission denied"),
	}}

	gs := gitOpsSetRendering("kept")
	gs.Status.Inventory = &templatesv1.ResourceInventory{Entries: []templatesv1.ResourceRef{
		mustResourceRef(t, kept),
		mustResourceRef(t, gone),
		mustResourceRef(t, stuck),
	}}

	reconciler := &GitOpsSetReconciler{}
	gens := map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	}

	inventory, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err == nil {
		t.Fatal("expected delete error")
	}

	wantIDs := []string{mustResourceRef(t, kept).ID, mustResourceRef(t, stuck).ID}
	if diff := cmp.Diff(wantIDs, inventoryIDs(inventory)); diff != "" {
		t.Fatalf("inventory mismatch (-want +got):\n%s", diff)
	}
	if err := base.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "gone"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("gone configmap get = %v, want NotFound", err)
	}
	if err := base.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "stuck"}, &corev1.ConfigMap{}); err != nil {
		t.Fatalf("stuck configmap should remain: %v", err)
	}

	cl.fail = nil
	inventory, err = reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gitOpsSetWithInventory(gs, inventory), gens)
	if err != nil {
		t.Fatalf("retry reconcile: %v", err)
	}
	wantIDs = []string{mustResourceRef(t, kept).ID}
	if diff := cmp.Diff(wantIDs, inventoryIDs(inventory)); diff != "" {
		t.Fatalf("inventory after retry mismatch (-want +got):\n%s", diff)
	}
	if err := base.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "stuck"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("stuck configmap get = %v, want NotFound", err)
	}
}

func TestRenderAndReconcileTreatsNotFoundDeleteAsSuccess(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	kept := newConfigMap("demo", "kept")
	missing := newConfigMap("demo", "missing")
	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(kept).Build()

	gs := gitOpsSetRendering("kept")
	gs.Status.Inventory = &templatesv1.ResourceInventory{Entries: []templatesv1.ResourceRef{
		mustResourceRef(t, kept),
		mustResourceRef(t, missing),
	}}

	reconciler := &GitOpsSetReconciler{}
	inventory, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{mustResourceRef(t, kept).ID}
	if diff := cmp.Diff(wantIDs, inventoryIDs(inventory)); diff != "" {
		t.Fatalf("inventory mismatch (-want +got):\n%s", diff)
	}
}

type deleteFailClient struct {
	client.Client
	fail map[string]error
}

func (c *deleteFailClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if err, ok := c.fail[obj.GetNamespace()+"/"+obj.GetName()]; ok && err != nil {
		return err
	}
	return c.Client.Delete(ctx, obj, opts...)
}

func newConfigMap(namespace, name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string]string{"ok": "yes"},
	}
}

func mustResourceRef(t *testing.T, obj runtime.Object) templatesv1.ResourceRef {
	t.Helper()
	ref, err := templatesv1.ResourceRefFromObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func inventoryIDs(inventory *templatesv1.ResourceInventory) []string {
	if inventory == nil {
		return nil
	}
	ids := make([]string, 0, len(inventory.Entries))
	for _, entry := range inventory.Entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

func gitOpsSetRendering(names ...string) *templatesv1.GitOpsSet {
	elements := make([]apiextensionsv1.JSON, 0, len(names))
	for _, name := range names {
		raw, err := json.Marshal(map[string]string{"name": name})
		if err != nil {
			panic(err)
		}
		elements = append(elements, apiextensionsv1.JSON{Raw: raw})
	}
	return &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "demo"},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{{
				List: &templatesv1.ListGenerator{Elements: elements},
			}},
			Templates: []templatesv1.GitOpsSetTemplate{{
				Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"{{ .Element.name }}","namespace":"demo"},"data":{"ok":"yes"}}`)},
			}},
		},
	}
}

func gitOpsSetWithInventory(gs *templatesv1.GitOpsSet, inventory *templatesv1.ResourceInventory) *templatesv1.GitOpsSet {
	copied := gs.DeepCopy()
	copied.Status.Inventory = inventory
	return copied
}
