package controllers

import (
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators/list"
)

func TestServerSideApplyUsesFieldManagerAndSkipsStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	cl := &applyOptionClient{Client: base}
	gs := gitOpsSetRendering("app")
	gs.Spec.Templates[0].Content.Raw = []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"{{ .Element.name }}","namespace":"demo"},"data":{"ok":"yes"},"status":{"phase":"applied"}}`)

	inventory, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), cl, gs, map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cl.manager != gitOpsSetFieldManager || cl.forced {
		t.Fatalf("manager=%q forced=%v", cl.manager, cl.forced)
	}
	if len(inventory.Entries) != 1 || inventory.Entries[0].LastAppliedTime == nil {
		t.Fatalf("inventory = %#v", inventory.Entries)
	}
	got := &unstructured.Unstructured{}
	got.SetAPIVersion("v1")
	got.SetKind("ConfigMap")
	if err := base.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "app"}, got); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(got.Object, "status"); found {
		t.Fatalf("status was applied: %#v", got.Object["status"])
	}
	data, _, _ := unstructured.NestedString(got.Object, "data", "ok")
	if data != "yes" {
		t.Fatalf("data = %#v", got.Object["data"])
	}
}

func TestServerSideApplyConflictRequiresForce(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := newConfigMap("demo", "kept")
	existing.Labels = map[string]string{"sets.gitops.pro/name": "set", "sets.gitops.pro/namespace": "demo"}
	base := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(existing).Build()
	cl := &applyOptionClient{Client: base, conflict: true}
	gs := gitOpsSetRendering("kept")
	gs.Status.Inventory = &templatesv1.ResourceInventory{Entries: []templatesv1.ResourceRef{mustResourceRef(t, existing)}}
	gens := map[string]generators.Generator{"List": list.NewGenerator(logr.Discard())}

	_, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err == nil || !apierrors.IsConflict(err) || cl.forced {
		t.Fatalf("err=%v forced=%v", err, cl.forced)
	}

	gs.Spec.Force = true
	if _, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens); err != nil {
		t.Fatal(err)
	}
	if !cl.forced {
		t.Fatal("force did not request field ownership")
	}
}

func TestServerSideApplyRejectsAnotherSetsResource(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := newConfigMap("demo", "kept")
	existing.Labels = map[string]string{"sets.gitops.pro/name": "other", "sets.gitops.pro/namespace": "demo"}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(existing).Build()
	gs := gitOpsSetRendering("kept")
	_, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), cl, gs, map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	})
	if err == nil || !strings.Contains(err.Error(), "owned by GitOpsSet demo/other") {
		t.Fatalf("err = %v", err)
	}
}

type applyOptionClient struct {
	client.Client
	manager  string
	forced   bool
	conflict bool
}

func (c *applyOptionClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	options := &client.PatchOptions{}
	options.ApplyOptions(opts)
	c.manager = options.FieldManager
	c.forced = options.Force != nil && *options.Force
	if c.conflict && !c.forced {
		return apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, obj.GetName(), context.Canceled)
	}
	return c.Client.Patch(ctx, obj, patch, opts...)
}
