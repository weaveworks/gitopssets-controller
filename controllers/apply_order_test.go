package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators/list"
)

func TestMissingNamespaceRequeuesWithoutApplyingDependentsFirst(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cl := &orderedCreateClient{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), missingNamespace: true}
	gs := gitOpsSetRendering("preview")
	gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
		{Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"app","namespace":"preview"},"data":{"ok":"yes"}}`)}},
		{Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"preview"}}`)}},
	}

	_, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), cl, gs, map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	})
	var retry *retryAfterError
	if !errors.As(err, &retry) || retry.after == 0 {
		t.Fatalf("err = %v", err)
	}
	if len(cl.created) == 0 || cl.created[0] != "Namespace/preview" {
		t.Fatalf("create order = %v", cl.created)
	}
}

type orderedCreateClient struct {
	client.Client
	created          []string
	missingNamespace bool
}

func (c *orderedCreateClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c.created = append(c.created, obj.GetObjectKind().GroupVersionKind().Kind+"/"+obj.GetName())
	if c.missingNamespace && obj.GetObjectKind().GroupVersionKind().Kind == "ConfigMap" {
		return apierrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, obj.GetNamespace())
	}
	return c.Client.Create(ctx, obj, opts...)
}
