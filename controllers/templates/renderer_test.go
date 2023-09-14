package templates

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/list"
	"github.com/weaveworks/gitopssets-controller/pkg/setup"
	"github.com/weaveworks/gitopssets-controller/test"
)

const (
	testGitOpsSetName = "test-gitops-set"
	testNS            = "demo"
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
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-dev-demo"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-prod-demo"), setClusterIP("192.168.100.20"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-prod"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-preprod-demo"), setClusterIP("192.168.150.30"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-preprod"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
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
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ sanitize .Element.env }}-demo", Namespace: testNS})),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineeringdev-demo"),
					setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
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
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": string("test-gitops-set"), "templates.weave.works/namespace": string("demo")}))),
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
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo1", Namespace: testNS})),
							},
						},
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo2", Namespace: testNS})),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-dev-demo1"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-dev-demo2"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-prod-demo1"), setClusterIP("192.168.100.20"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-prod"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-prod-demo2"), setClusterIP("192.168.100.20"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-prod"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
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
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo1", Namespace: "{{ .GitOpsSet.Namespace }}"}, setClusterIP("{{ first .Element.ips }}"))),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-dev-demo1"), setClusterIP("192.168.0.252"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set",
						"templates.weave.works/namespace": testNS}))),
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
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env}}-demo1", Namespace: testNS})),
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
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-dev-demo1"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
				test.ToUnstructured(t, makeTestNamespace("testing1-engineering-dev",
					addLabels[*corev1.Namespace](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
				test.ToUnstructured(t, makeTestNamespace("testing2-engineering-dev",
					addLabels[*corev1.Namespace](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
			},
		},
		{
			name: "repeat elements with no elements does not error",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","externalIP": "192.168.50.50","namespaces":null}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env}}-demo1", Namespace: testNS})),
							},
						},
						{
							Repeat: "{ $.namespaces }",
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestNamespace("{{ .Repeat.name }}-{{ .Element.env }}")),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-dev-demo1"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
			},
		},
		{
			name: "repeat elements with maps",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"wge":{"mgmt-repo":"example-repo","gui":false},"template":{"name":"dynamic-v1","namespace":"default"},"aws":{"region":"us-west-2"},"vpcs":[{"name":"v1","mode":"create","cidr":"10.0.0.0/16","publicsubnets":3,"privatesubnets":3}],"clusters":[{"name":"nested-cluster","mode":"create","gui":true,"vpc_name":"v1","version":1.23,"apps":[{"name":"example","version":"0.0.4"},{"name":"testing"}]}]}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Repeat: "{ $.clusters[?(@.gui)] }",
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestNamespace("{{ .Repeat.name }}")),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestNamespace("nested-cluster", addLabels[*corev1.Namespace](map[string]string{
					"templates.weave.works/name":      "test-gitops-set",
					"templates.weave.works/namespace": "demo",
				},
				))),
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
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo1", Namespace: testNS},
									addLabels[*corev1.Service](map[string]string{"templates.weave.works/test": "test-value"}))),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-dev-demo1"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS, "templates.weave.works/test": "test-value"}))),
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-prod-demo1"), setClusterIP("192.168.100.20"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-prod"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS, "templates.weave.works/test": "test-value"}))),
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
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo1", Namespace: testNS},
									addLabels[*corev1.Service](map[string]string{"templates.weave.works/namespace": "new-ns"}))),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-dev-demo1"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": "new-ns"}))),
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-prod-demo1"), setClusterIP("192.168.100.20"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-prod"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": "new-ns"}))),
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
								Raw: []byte(`{"kind":"Service","apiVersion":"v1","metadata":{"name":"{{ getordefault .Element \"name\" \"defaulted\" }}-demo","namespace":"testing","creationTimestamp":null,"annotations":{"app.kubernetes.io/instance":"{{ .Element.env }}"}},"spec":{"ports":[{"name":"http","protocol":"TCP","port":8080,"targetPort":8080}],"clusterIP":"{{ .Element.externalIP }}"}}`),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn("testing", "defaulted-demo"),
					setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
			},
		},
		{
			name: "populated with namespace if rendered template has no namespace",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering dev","externalIP": "192.168.50.50"}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.ObjectMeta.Namespace = "template-ns"
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								// Note that the NamespacedName has no Namespace
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "testing-no-ns"})),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn("template-ns", "testing-no-ns"),
					setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": "template-ns"}))),
			},
		},
		{
			name: "templating numbers",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","replicas": 2}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.ObjectMeta.Annotations = map[string]string{
						"templates.weave.works/delimiters": "${{,}}",
					}
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: []byte(`{"kind":"Deployment","apiVersion":"apps/v1","metadata":{"name":"${{ .Element.env }}-demo"},"spec":{"version": "1.21", "enabled": "true", "light": "on", "replicas": "${{ .Element.replicas }}" } }`),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"metadata": map[string]interface{}{
							"name":      "engineering-dev-demo",
							"namespace": "demo",
							"labels": map[string]interface{}{
								"templates.weave.works/name":      "test-gitops-set",
								"templates.weave.works/namespace": "demo",
							},
						},
						"spec": map[string]interface{}{
							// Type should be a number here, not a string
							"replicas": int64(2),
							// check that other string/bool values stay as strings
							"light":   "on",
							"enabled": "true",
							"version": "1.21",
						},
					},
				},
			},
		},
		{
			name: "templating objects",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","sourceRef": { "kind": "GitRepository", "name": "my-git-repo" }}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.ObjectMeta.Annotations = map[string]string{
						"templates.weave.works/delimiters": "${{,}}",
					}
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: []byte(`{"kind":"Kustomization","metadata":{"name":"${{ .Element.env }}-demo"},"spec":{"sourceRef": "${{ .Element.sourceRef | toJson }}" } }`),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind": "Kustomization",
						"metadata": map[string]interface{}{
							"name":      "engineering-dev-demo",
							"namespace": "demo",
							"labels": map[string]interface{}{
								"templates.weave.works/name":      "test-gitops-set",
								"templates.weave.works/namespace": "demo",
							},
						},
						"spec": map[string]interface{}{
							"sourceRef": map[string]interface{}{
								"kind": "GitRepository",
								"name": "my-git-repo",
							},
						},
					},
				},
			},
		},
		{
			name: "toYaml function",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env":"testing","depends":[{"name":"testing"}]}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.ObjectMeta.Annotations = map[string]string{
						"templates.weave.works/delimiters": "((,))",
					}
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: []byte(`{"apiVersion":"kustomize.toolkit.fluxcd.io/v1beta2","kind":"Kustomization","metadata":{"name":"testing-demo"},"spec":{"dependsOn":"(( .Element.depends | toYaml | nindent 8 ))","interval":"5m","path":"./examples/kustomize/environments/(( .Element.env ))","prune":true,"sourceRef":{"kind":"GitRepository","name":"go-demo-repo"}}}`),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "kustomize.toolkit.fluxcd.io/v1beta2",
						"kind":       "Kustomization",
						"metadata": map[string]interface{}{
							"name":      "testing-demo",
							"namespace": "demo",
							"labels": map[string]interface{}{
								"templates.weave.works/name":      "test-gitops-set",
								"templates.weave.works/namespace": "demo",
							},
						},
						"spec": map[string]interface{}{
							"dependsOn": []any{
								map[string]any{
									"name": "testing",
								},
							},
							"interval": "5m",
							"path":     "./examples/kustomize/environments/testing",
							"prune":    true,
							"sourceRef": map[string]interface{}{
								"kind": "GitRepository",
								"name": "go-demo-repo",
							},
						},
					},
				},
			},
		},
		{
			name: "repeat elements with cel",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","externalIP": "192.168.50.50","namespaces":["testing1","testing2"]}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env}}-demo1", Namespace: testNS})),
							},
						},
						{
							Repeat: "cel:Element.namespaces",
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestNamespace("{{ .Repeat }}-{{ .Element.env }}")),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestService(nsn(testNS, "engineering-dev-demo1"), setClusterIP("192.168.50.50"),
					addAnnotations(map[string]string{"app.kubernetes.io/instance": "engineering-dev"}),
					addLabels[*corev1.Service](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
				test.ToUnstructured(t, makeTestNamespace("testing1-engineering-dev",
					addLabels[*corev1.Namespace](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
				test.ToUnstructured(t, makeTestNamespace("testing2-engineering-dev",
					addLabels[*corev1.Namespace](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
			},
		},
		{
			name: "repeat elements with cel objects",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","externalIP": "192.168.50.50","items":[{"namespace":"testing1"},{"namespace":"testing2"}]}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Repeat: "cel:Element.items",
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestNamespace("{{ .Repeat.namespace }}-{{ .Element.env }}")),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestNamespace("testing1-engineering-dev",
					addLabels[*corev1.Namespace](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
				test.ToUnstructured(t, makeTestNamespace("testing2-engineering-dev",
					addLabels[*corev1.Namespace](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
			},
		},
		{
			name: "repeat elements with cel filtering",
			elements: []apiextensionsv1.JSON{
				{Raw: []byte(`{"env": "engineering-dev","externalIP": "192.168.50.50","items":[{"namespace":"testing1"},{"namespace":"testing2"}]}`)},
			},
			setOptions: []func(*templatesv1.GitOpsSet){
				func(s *templatesv1.GitOpsSet) {
					s.Spec.Templates = []templatesv1.GitOpsSetTemplate{
						{
							Repeat: "cel:Element.items.filter(x, x.namespace.endsWith('2'))",
							Content: runtime.RawExtension{
								Raw: mustMarshalJSON(t, makeTestNamespace("{{ .Repeat.namespace }}-{{ .Element.env }}")),
							},
						},
					}
				},
			},
			want: []*unstructured.Unstructured{
				test.ToUnstructured(t, makeTestNamespace("testing2-engineering-dev",
					addLabels[*corev1.Namespace](map[string]string{"templates.weave.works/name": "test-gitops-set", "templates.weave.works/namespace": testNS}))),
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

func TestRender_files(t *testing.T) {
	testGenerators := map[string]generators.Generator{
		"List": list.NewGenerator(logr.Discard()),
	}

	generatorTests := []struct {
		filename string
		want     string
	}{
		{
			filename: "testdata/template-with-repeat-index.yaml",
			want:     "testdata/template-with-repeat-index-rendered.yaml",
		},
		{
			filename: "testdata/template-with-element-index.yaml",
			want:     "testdata/template-with-element-index-rendered.yaml",
		},
		{
			filename: "testdata/template-with-top-level-elements.yaml",
			want:     "testdata/template-with-top-level-elements-rendered.yaml",
		},
	}

	for _, tt := range generatorTests {
		t.Run(tt.filename, func(t *testing.T) {
			gset := readFixtureAsGitOpsSet(t, tt.filename)
			objs, err := Render(context.TODO(), gset, testGenerators)
			test.AssertNoError(t, err)

			assertFixturesMatch(t, tt.want, objs)
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
								Raw: []byte(`"{{ .test | tested }}"`),
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

func TestRender_disabled(t *testing.T) {
	gset := makeTestGitOpsSet(t)
	// no generators available
	testGenerators := map[string]generators.Generator{}
	res, err := Render(context.TODO(), gset, testGenerators)
	test.AssertNoError(t, err)
	if cmp.Diff([]*unstructured.Unstructured{}, res) != "" {
		t.Fatalf("expected no resources to be rendered")
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
			Namespace: testNS,
		},
		Spec: templatesv1.GitOpsSetSpec{
			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, makeTestService(types.NamespacedName{Name: "{{ .Element.env }}-demo", Namespace: testNS})),
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

type labelSetter interface {
	SetLabels(map[string]string)
}

func addLabels[T labelSetter](labels map[string]string) func(T) {
	return func(s T) {
		s.SetLabels(labels)
	}
}

func nsn(namespace, name string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

func readFixtureAsGitOpsSet(t *testing.T, filename string) *templatesv1.GitOpsSet {
	t.Helper()
	scheme, err := setup.NewSchemeForGenerators(setup.DefaultGenerators)
	test.AssertNoError(t, err)

	b, err := os.ReadFile(filename)
	test.AssertNoError(t, err)

	m, _, err := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(b, nil, nil)
	test.AssertNoError(t, err)

	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(m)
	test.AssertNoError(t, err)

	u := &unstructured.Unstructured{Object: raw}
	newObj, err := scheme.New(u.GetObjectKind().GroupVersionKind())
	test.AssertNoError(t, err)

	set, err := newObj.(*templatesv1.GitOpsSet), scheme.Convert(u, newObj, nil)
	test.AssertNoError(t, err)

	return set
}

func assertFixturesMatch(t *testing.T, filename string, objs []*unstructured.Unstructured) {
	// t.Helper()

	fixture, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %s", filename, err)
	}

	want := []*unstructured.Unstructured{}
	for _, raw := range bytes.Split(fixture, []byte("---")) {
		if t := bytes.TrimSpace(raw); len(t) == 0 {
			continue
		}

		m, _, err := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(raw, nil, nil)
		test.AssertNoError(t, err)

		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(m)
		test.AssertNoError(t, err)

		u := &unstructured.Unstructured{Object: raw}
		want = append(want, u)
	}

	if diff := cmp.Diff(want, objs); diff != "" {
		t.Fatalf("failed to match fixtures:\n%s", diff)
	}
}
