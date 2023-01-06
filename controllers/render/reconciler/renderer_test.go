package reconciler

import (
	"context"
	"testing"
	"time"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta2"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	templatesv1 "github.com/weaveworks/gitops-sets-controller/api/v1alpha1"
	"github.com/weaveworks/gitops-sets-controller/controllers/render/generators"
	"github.com/weaveworks/gitops-sets-controller/controllers/render/generators/list"
	"github.com/weaveworks/gitops-sets-controller/test"
)

const (
	testGitOpsSetName      = "test-kustomizations"
	testGitOpsSetNamespace = "demo"
)

func TestRenderTemplates(t *testing.T) {
	testGenerators := map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	}

	listGeneratorTests := []struct {
		name     string
		elements []apiextensionsv1.JSON
		want     []runtime.Object
	}{
		{
			name: "multiple elements",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"cluster": "engineering-dev"}`)},
				{Raw: []byte(`{"cluster": "engineering-prod"}`)},
				{Raw: []byte(`{"cluster": "engineering-preprod"}`)},
			},
			want: []runtime.Object{
				newTestUnstructured(t, makeTestKustomization(nsn("demo", "engineering-dev-demo"))),
				newTestUnstructured(t, makeTestKustomization(nsn("demo", "engineering-prod-demo"))),
				newTestUnstructured(t, makeTestKustomization(nsn("demo", "engineering-preprod-demo"))),
			},
		},
	}

	for _, tt := range listGeneratorTests {
		t.Run(tt.name, func(t *testing.T) {
			gset := makeTestGitOpsSet(t, withListElements(tt.elements))
			objs, err := RenderTemplates(context.TODO(), gset, testGenerators)
			test.AssertNoError(t, err)

			if diff := cmp.Diff(tt.want, objs); diff != "" {
				t.Fatalf("failed to generate resources:\n%s", diff)
			}
		})
	}
}

func withListElements(el []apiextensionsv1.JSON) func(*templatesv1.GitOpsSet) {
	return func(gs *templatesv1.GitOpsSet) {
		if gs.Spec.Generators == nil {
			gs.Spec.Generators = []templatesv1.GitOpsSetGenerator{}
		}
		gs.Spec.Generators = append(gs.Spec.Generators,
			templatesv1.GitOpsSetGenerator{
				List: &templatesv1.ListGenerator{
					Elements: el,
				},
			})
	}
}

func makeTestGitOpsSet(t *testing.T, opts ...func(*templatesv1.GitOpsSet)) *templatesv1.GitOpsSet {
	ks := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testGitOpsSetName,
			Namespace: testGitOpsSetNamespace,
		},
		Spec: templatesv1.GitOpsSetSpec{
			Template: templatesv1.GitOpsSetTemplate{
				runtime.RawExtension{
					Raw: mustMarshalYAML(t, makeTestKustomization(types.NamespacedName{Name: "{{.cluster}}-demo"})),
				},
			},
		},
	}
	for _, o := range opts {
		o(ks)
	}

	return ks
}

func mustMarshalYAML(t *testing.T, r runtime.Object) []byte {
	b, err := yaml.Marshal(r)
	test.AssertNoError(t, err)

	return b
}

func withLabels(labels map[string]string) func(runtime.Object) {
	accessor := apimeta.NewAccessor()
	return func(obj runtime.Object) {
		accessor.SetLabels(obj, labels)
	}
}

// func Test_renderTemplateParams(t *testing.T) {
// 	templateTests := []struct {
// 		name   string
// 		tmpl   *kustomizev1.Kustomization
// 		params map[string]any
// 		want   *kustomizev1.Kustomization
// 	}{
// 		{name: "no params", tmpl: newKustomization(), want: newKustomization()},
// 		{
// 			name:   "simple params",
// 			tmpl:   newKustomization(templatePath("{{.replaced}}")),
// 			params: map[string]any{"replaced": "new string"},
// 			want:   newKustomization(templatePath("new string")),
// 		},
// 		{
// 			name:   "sanitize",
// 			tmpl:   newKustomization(templatePath("{{ sanitize .replaced }}")),
// 			params: map[string]any{"replaced": "new string"},
// 			want:   newKustomization(templatePath("newstring")),
// 		},
// 	}

// 	for _, tt := range templateTests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			rendered, err := renderTemplateParams(tt.tmpl, tt.params)
// 			test.AssertNoError(t, err)

// 			if diff := cmp.Diff(tt.want, rendered); diff != "" {
// 				t.Fatalf("rendering failed:\n%s", diff)
// 			}
// 		})
// 	}
// }

// func TestRenderTemplate_errors(t *testing.T) {
// 	templateTests := []struct {
// 		name    string
// 		tmpl    *kustomizev1.Kustomization
// 		params  map[string]any
// 		wantErr string
// 	}{
// 		{name: "no template", tmpl: nil, params: nil, wantErr: "template is empty"},
// 	}

// 	for _, tt := range templateTests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			_, err := renderTemplateParams(tt.tmpl, tt.params)

// 			test.AssertErrorMatch(t, tt.wantErr, err)
// 		})
// 	}
// }

func nsn(namespace, name string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

func newTestUnstructured(t *testing.T, obj runtime.Object) *unstructured.Unstructured {
	t.Helper()
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		t.Fatal(err)
	}
	delete(raw, "status")

	return &unstructured.Unstructured{Object: raw}
}

// TODO: Change this to a different resource type!
func makeTestKustomization(name types.NamespacedName, opts ...func(*kustomizev1.Kustomization)) *kustomizev1.Kustomization {
	k := kustomizev1.Kustomization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Kustomization",
			APIVersion: "kustomize.toolkit.fluxcd.io/v1beta2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name + "-demo",
			Namespace: name.Namespace,
		},
		Spec: kustomizev1.KustomizationSpec{
			Path:     "./clusters/" + name.Name + "/",
			Interval: metav1.Duration{Duration: 5 * time.Minute},
			Prune:    true,
			SourceRef: kustomizev1.CrossNamespaceSourceReference{
				Kind: "GitRepository",
				Name: "demo-repo",
			},
			KubeConfig: &meta.KubeConfigReference{
				SecretRef: meta.SecretKeyReference{
					Name: name.Name,
				},
			},
		},
	}
	for _, o := range opts {
		o(&k)
	}

	return &k
}
