package templates

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators/list"
)

func TestFilterElements(t *testing.T) {
	elements := []map[string]any{
		{"name": "keep", "env": "prod"},
		{"name": "drop", "env": "dev"},
	}
	got, err := filterElements(2, `element.name == "keep"`, elements)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["name"] != "keep" {
		t.Fatalf("filtered = %#v", got)
	}

	got, err = filterElements(0, "", elements)
	if err != nil || len(got) != 2 {
		t.Fatalf("empty filter = %#v, err = %v", got, err)
	}

	_, err = filterElements(4, "element.name", elements)
	if err == nil || !strings.Contains(err.Error(), "generator 4") || !strings.Contains(err.Error(), "boolean") {
		t.Fatalf("err = %v", err)
	}
	_, err = filterElements(1, "element.missing(", elements)
	if err == nil || !strings.Contains(err.Error(), "generator 1") {
		t.Fatalf("err = %v", err)
	}
}

func TestRenderAppliesFilterBeforeTemplates(t *testing.T) {
	set := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "demo"},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{{
				Filter: `element.env == "prod"`,
				List: &templatesv1.ListGenerator{Elements: []apiextensionsv1.JSON{
					{Raw: []byte(`{"env":"dev","name":"dev-app"}`)},
					{Raw: []byte(`{"env":"prod","name":"prod-app"}`)},
				}},
			}},
			Templates: []templatesv1.GitOpsSetTemplate{{
				Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"{{ .Element.name }}","namespace":"demo"}}`)},
			}},
		},
	}
	rendered, err := Render(t.Context(), set, map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered) != 1 || rendered[0].GetName() != "prod-app" {
		t.Fatalf("rendered = %#v", names(rendered))
	}
}

func names(objs []*unstructured.Unstructured) []string {
	out := make([]string, 0, len(objs))
	for _, obj := range objs {
		out = append(out, obj.GetName())
	}
	return out
}
