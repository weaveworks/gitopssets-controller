package templates

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/list"
	"github.com/weaveworks/gitopssets-controller/test"
)

const (
	testGitOpsSetName      = "test-gitops-set"
	testGitOpsSetNamespace = "demo"
)

func TestRender(t *testing.T) {
	testGenerators := map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	}

	generatorTests := []struct {
		name       string
		elements   []apiextensionsv1.JSON
		setOptions []func(*templatesv1.GitOpsSet)
		want       []*unstructured.Unstructured
	}{
		{
			name: "multiple elements",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","externalIP": "192.168.50.50"}`)},
				{Raw: []byte(`{"env": "engineering-prod","externalIP": "192.168.100.20"}`)},
				{Raw: []byte(`{"env": "engineering-preprod","externalIP": "192.168.150.30"}`)},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-dev-demo"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-dev")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-prod-demo"), setClusterIP("192.168.100.20"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-prod")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-preprod-demo"), setClusterIP("192.168.150.30"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-preprod")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
			},
		},

		{
			name: "sanitization",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering dev","externalIP": "192.168.50.50"}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ sanitize .Element.env }}-demo"})),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineeringdev-demo"),
					setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering dev")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
			},
		},
		{
			name: "custom delimiters",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering","externalIP": "192.168.50.50"}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.ObjectMeta.Annotations = map[string]string{
						"templates.weave.works/delimiters": "$[,]",
					}
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "$[ .Element.env ]-demo"}, func(s *corev1.Service) {
									s.ObjectMeta.Annotations = map[string]string{
										"app.kubernetes.io/instance": "$[ .Element.env ]",
									}
									s.Spec.ClusterIP = "$[ .Element.externalIP ]"
								})),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-demo"),
					setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
			},
		},
		{
			name: "multiple templates yields cartesian result",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","externalIP": "192.168.50.50"}`)},
				{Raw: []byte(`{"env": "engineering-prod","externalIP": "192.168.100.20"}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo1"})),
							},
						},
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo2"})),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-dev-demo1"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-dev")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-dev-demo2"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-dev")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-prod-demo1"), setClusterIP("192.168.100.20"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-prod")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-prod-demo2"), setClusterIP("192.168.100.20"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-prod")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
			},
		},
		{
			name: "sprig functions are available",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","ips":["192.168.0.252","192.168.0.253"]}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo1"}, setClusterIP("{{ first .Element.ips }}"))),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-dev-demo1"), setClusterIP("192.168.0.252"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-dev")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
			},
		},
		{
			name: "repeat elements",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","externalIP": "192.168.50.50","namespaces":["testing1","testing2"]}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env}}-demo1"})),
							},
						},
						{
							Repeat: "{ $.namespaces }",
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestNamespace("{{ .Repeat }}-{{ .Element.env }}")),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-dev-demo1"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-dev")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
				test.ToUnstructured(t, makeTestNamespace("testing1-engineering-dev")),
				test.ToUnstructured(t, makeTestNamespace("testing2-engineering-dev")),
			},
		},
		{
			name: "template with labels merged with default labels",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","externalIP": "192.168.50.50"}`)},
				{Raw: []byte(`{"env": "engineering-prod","externalIP": "192.168.100.20"}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo1"}, addLabels(map[string]string{"templates.weave.works/test": string("test-value")}))),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-dev-demo1"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-dev")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo"), "templates.weave.works/test": string("test-value")}))),
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-prod-demo1"), setClusterIP("192.168.100.20"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-prod")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo"), "templates.weave.works/test": string("test-value")}))),
			},
		},
		{
			name: "labels overriding default labels",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","externalIP": "192.168.50.50"}`)},
				{Raw: []byte(`{"env": "engineering-prod","externalIP": "192.168.100.20"}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo1"}, addLabels(map[string]string{"templates.weave.works/namespace": string("new-ns")}))),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-dev-demo1"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-dev")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("new-ns")}))),
				test.ToUnstructured(t, makeTestService(nsn("demo", "engineering-prod-demo1"), setClusterIP("192.168.100.20"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering-prod")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("new-ns")}))),
			},
		},
		{
			name: "defaulting values",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering dev","externalIP": "192.168.50.50"}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: []byte(`{"kind":"Service","apiVersion":"v1","metadata":{"name":"{{ getordefault .Element "name" "defaulted" }}-demo","creationTimestamp":null,"annotations":{"app.kubernetes.io/instance":"{{ .Element.env }}"}},"spec":{"ports":[{"name":"http","protocol":"TCP","port":8080,"targetPort":8080}],"clusterIP":"{{ .Element.externalIP }}"}}`),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn("demo", "defaulted-demo"),
					setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": string("engineering dev")}),
					addLabels(map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
			},
		},
	}

	for _, tt := range generatorTests {
		t.Run(tt.name, func(t *testing.T) {
			gset := makeTestGitOpsSet(t, append(tt.setOptions, listElements(tt.elements))...)
			objs, err := Render(context.TODO(), gset, testGenerators)
			test.AssertNoError(t, err)

			if diff := cmp.Diff(tt.want, objs); diff != "" {
				t.Fatalf("failed to generate resources:\n%s", diff)
			}
		})
	}
}

func TestRender_errors(t *testing.T) {
	templateTests := []struct {
		name       string
		setOptions []func(*templatesv1.GitOpsSet)
		wantErr    string
	}{
		{
			name: "bad template",
			setOptions: []func(*templatesv1.GitOpsSet){
				func(gs *templatesv1.GitOpsSet) {
					gs.Spec.Generators = []templatesv1.GitOpsSetGenerator{
						{
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"env": "engineering-dev","externalIP": "192.168.50.50"}`)},
								},
							},
						},
					}
					gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: []byte("{{ .test | tested }}"),
							},
						},
					}
				},
			},
			wantErr: `failed to parse template: template: demo/test-gitops-set:1: function "tested" not defined`,
		},
		{
			name: "missing key in template",
			setOptions: []func(*templatesv1.GitOpsSet){
				func(gs *templatesv1.GitOpsSet) {
					gs.Spec.Generators = []templatesv1.GitOpsSetGenerator{
						{
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"env": "engineering-dev","externalIP": "192.168.50.50"}`)},
								},
							},
						},
					}

					gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .element.env }}-demo"})),
							},
						},
					}
				},
			},
			wantErr: `failed to render template.*at <.element.env>: map has no entry for key "element"`,
		},
	}
	testGenerators := map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	}

	for _, tt := range templateTests {
		t.Run(tt.name, func(t *testing.T) {
			gset := makeTestGitOpsSet(t, tt.setOptions...)
			_, err := Render(context.TODO(), gset, testGenerators)

			test.AssertErrorMatch(t, tt.wantErr, err)
		})
	}
}

func listElements(el []apiextensionsv1.JSON) func(*templatesv1.GitOpsSet) {
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
			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo"})),
					},
				},
			},
		},
	}
	for _, o := range opts {
		o(ks)
	}

	return ks
}

func makeTestService(name types.NamespacedName, opts ...func(*corev1.Service)) *corev1.Service {
	s := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
			Annotations: map[string]string{
				"app.kubernetes.io/instance": "{{ .Element.env }}",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "{{ .Element.externalIP }}",
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       8080,
					TargetPort: intstr.FromInt(8080)},
			},
		},
	}
	for _, o := range opts {
		o(&s)
	}

	return &s
}

func makeTestNamespace(name string, opts ...func(*corev1.Namespace)) *corev1.Namespace {
	n := corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	for _, o := range opts {
		o(&n)
	}

	return &n
}

func setClusterIP(ip string) func(s *corev1.Service) {
	return func(s *corev1.Service) {
		s.Spec.ClusterIP = ip
	}
}

func mustMarshalJSON(t *testing.T, r runtime.Object) []byte {
	b, err := json.Marshal(r)
	test.AssertNoError(t, err)

	return b
}

func addAnnotations(ann map[string]string) func(*corev1.Service) {
	return func(s *corev1.Service) {
		s.SetAnnotations(ann)
	}
}

func addLabels(label map[string]string) func(*corev1.Service) {
	return func(s *corev1.Service) {
		s.SetLabels(label)
	}
}

func nsn(namespace, name string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}
