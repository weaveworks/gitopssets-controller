package templates

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators/list"
)

func TestRenderWithMapperAssignsScope(t *testing.T) {
	mapper := scopeMapper{namespaced: map[schema.GroupKind]bool{
		{Group: "", Kind: "Service"}:                              true,
		{Group: "rbac.authorization.k8s.io", Kind: "ClusterRole"}: false,
	}}
	gens := map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	}

	t.Run("namespaced resource inherits the GitOpsSet namespace", func(t *testing.T) {
		set := scopeGitOpsSet(t, `{"apiVersion":"v1","kind":"Service","metadata":{"name":"web"},"spec":{"ports":[{"port":80}]}}`)
		got, err := RenderWithMapper(t.Context(), set, gens, mapper)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("rendered %d objects, want 1", len(got))
		}
		if got[0].GetNamespace() != "demo" {
			t.Fatalf("namespace = %q, want demo", got[0].GetNamespace())
		}
	})

	t.Run("cluster-scoped resource keeps an empty namespace", func(t *testing.T) {
		set := scopeGitOpsSet(t, `{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"web"},"rules":[]}`)
		got, err := RenderWithMapper(t.Context(), set, gens, mapper)
		if err != nil {
			t.Fatal(err)
		}
		if got[0].GetNamespace() != "" {
			t.Fatalf("namespace = %q, want empty", got[0].GetNamespace())
		}
	})

	t.Run("cluster-scoped resource rejects a namespace", func(t *testing.T) {
		set := scopeGitOpsSet(t, `{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"web","namespace":"demo"},"rules":[]}`)
		_, err := RenderWithMapper(t.Context(), set, gens, mapper)
		if err == nil || !strings.Contains(err.Error(), "ClusterRole") {
			t.Fatalf("error = %v, want an error naming ClusterRole", err)
		}
	})

	t.Run("unknown kind fails the render", func(t *testing.T) {
		set := scopeGitOpsSet(t, `{"apiVersion":"example.com/v1","kind":"Widget","metadata":{"name":"web"}}`)
		_, err := RenderWithMapper(t.Context(), set, gens, mapper)
		if err == nil || !strings.Contains(err.Error(), "Widget") {
			t.Fatalf("error = %v, want an error naming Widget", err)
		}
	})
}

func scopeGitOpsSet(t *testing.T, template string) *templatesv1.GitOpsSet {
	t.Helper()
	return &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "demo"},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{{
				List: &templatesv1.ListGenerator{Elements: []apiextensionsv1.JSON{{Raw: []byte(`{"name":"web"}`)}}},
			}},
			Templates: []templatesv1.GitOpsSetTemplate{{
				Content: runtime.RawExtension{Raw: []byte(template)},
			}},
		},
	}
}

type scopeMapper struct {
	namespaced map[schema.GroupKind]bool
}

func (m scopeMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	namespaced, ok := m.namespaced[gk]
	if !ok {
		return nil, fmt.Errorf("no mapping for %s", gk)
	}
	scope := meta.RESTScopeRoot
	if namespaced {
		scope = meta.RESTScopeNamespace
	}
	version := ""
	if len(versions) > 0 {
		version = versions[0]
	}
	return &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Group: gk.Group, Version: version, Resource: strings.ToLower(gk.Kind) + "s"},
		GroupVersionKind: schema.GroupVersionKind{Group: gk.Group, Version: version, Kind: gk.Kind},
		Scope:            scope,
	}, nil
}

func (m scopeMapper) KindFor(schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, fmt.Errorf("not implemented")
}
func (m scopeMapper) KindsFor(schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m scopeMapper) ResourceFor(schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, fmt.Errorf("not implemented")
}
func (m scopeMapper) ResourcesFor(schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m scopeMapper) RESTMappings(schema.GroupKind, ...string) ([]*meta.RESTMapping, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m scopeMapper) ResourceSingularizer(string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
