package matrix

import (
	"context"
	"testing"
	"time"

	"github.com/fluxcd/pkg/http/fetch"
	"github.com/fluxcd/pkg/tar"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators/gitrepository"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators/list"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators/pullrequests"
	"github.com/gitops-tools/gitopssets-controller/test"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testNamespace = "generation"

func TestMatrixGenerator_Generate(t *testing.T) {
	srv := test.StartFakeArchiveServer(t, "testdata")
	gr := &templatesv1.GitRepositoryGenerator{
		RepositoryRef: "test-repository",
		Files: []templatesv1.RepositoryGeneratorFileItem{
			{Path: "files/dev.yaml"},
			{Path: "files/production.yaml"},
			{Path: "files/staging.yaml"},
		},
	}
	tests := []struct {
		name             string
		sg               *templatesv1.GitOpsSetGenerator
		ks               *templatesv1.GitOpsSet
		objects          []runtime.Object
		expectedMatrix   []map[string]any
		expectedErrorStr string
	}{
		{
			name:             "nil sg",
			sg:               nil,
			ks:               nil,
			expectedErrorStr: "GitOpsSet is empty",
		},
		{
			name: "nil matrix",
			sg: &templatesv1.GitOpsSetGenerator{
				Matrix: nil,
			},
			ks:               nil,
			expectedMatrix:   nil,
			expectedErrorStr: "",
		},
		{
			name: "less than 2 generators",
			sg: &templatesv1.GitOpsSetGenerator{
				Matrix: &templatesv1.MatrixGenerator{
					Generators: []templatesv1.GitOpsSetNestedGenerator{
						{
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"key1": "value1"}`)},
									{Raw: []byte(`{"key2": "value2"}`)},
								},
							},
						},
					},
				},
			},
			ks: nil,
			expectedMatrix: []map[string]any{
				{"key1": "value1"},
				{"key2": "value2"},
			},
		},
		{
			name: "valid matrix",
			sg: &templatesv1.GitOpsSetGenerator{
				Matrix: &templatesv1.MatrixGenerator{
					Generators: []templatesv1.GitOpsSetNestedGenerator{
						{
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"cluster": "cluster","url": "url"}`)},
								},
							},
						},
						{
							GitRepository: gr,
						},
					},
				},
			},
			ks: &templatesv1.GitOpsSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-generator",
					Namespace: testNamespace,
				},
				Spec: templatesv1.GitOpsSetSpec{
					Generators: []templatesv1.GitOpsSetGenerator{
						{
							GitRepository: gr,
						},
					},
				},
			},
			objects: []runtime.Object{newGitRepository(srv.URL+"/files.tar.gz",
				"sha256:f0a57ec1cdebda91cf00d89dfa298c6ac27791e7fdb0329990478061755eaca8")},
			expectedMatrix: []map[string]any{
				{
					"cluster":     "cluster",
					"environment": "dev",
					"instances":   2.0,
					"url":         "url",
				},
				{
					"cluster":     "cluster",
					"environment": "production",
					"instances":   10.0,
					"url":         "url",
				},
				{
					"cluster":     "cluster",
					"environment": "staging",
					"instances":   5.0,
					"url":         "url",
				},
			},
			expectedErrorStr: "",
		},
		{
			name: "valid matrix - one generator generates 0 elements",
			sg: &templatesv1.GitOpsSetGenerator{
				Matrix: &templatesv1.MatrixGenerator{
					Generators: []templatesv1.GitOpsSetNestedGenerator{
						{
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"cluster": "cluster","url": "url"}`)},
								},
							},
						},
						{
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{},
							},
						},
					},
				},
			},
			ks: &templatesv1.GitOpsSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-generator",
					Namespace: testNamespace,
				},
				Spec: templatesv1.GitOpsSetSpec{
					Generators: []templatesv1.GitOpsSetGenerator{},
				},
			},
			expectedMatrix: []map[string]any{
				{
					"cluster": "cluster",
					"url":     "url",
				},
			},
			expectedErrorStr: "",
		},
		{
			name: "valid matrix - all generators generates no elements",
			sg: &templatesv1.GitOpsSetGenerator{
				Matrix: &templatesv1.MatrixGenerator{
					Generators: []templatesv1.GitOpsSetNestedGenerator{
						{
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{},
							},
						},
						{
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{},
							},
						},
					},
				},
			},
			ks: &templatesv1.GitOpsSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-generator",
					Namespace: testNamespace,
				},
				Spec: templatesv1.GitOpsSetSpec{
					Generators: []templatesv1.GitOpsSetGenerator{},
				},
			},
			expectedMatrix:   []map[string]any{},
			expectedErrorStr: "",
		},
		{
			name: "naming nested elements",
			sg: &templatesv1.GitOpsSetGenerator{
				Matrix: &templatesv1.MatrixGenerator{
					Generators: []templatesv1.GitOpsSetNestedGenerator{
						{
							Name: "list1",
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"key1": "value1"}`)},
									{Raw: []byte(`{"key2": "value2"}`)},
								},
							},
						},
						{
							Name: "list2",
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"key1": "value3"}`)},
									{Raw: []byte(`{"key2": "value4"}`)},
								},
							},
						},
					},
				},
			},
			expectedMatrix: []map[string]any{
				{"list1": map[string]any{"key1": "value1"},
					"list2": map[string]any{"key1": "value3"}},
				{"list1": map[string]any{"key1": "value1"},
					"list2": map[string]any{"key2": "value4"}},
				{"list1": map[string]any{"key2": "value2"},
					"list2": map[string]any{"key1": "value3"}},
				{"list1": map[string]any{"key2": "value2"},
					"list2": map[string]any{"key2": "value4"}},
			},
		},
		{
			name: "naming nested elements with three generators",
			sg: &templatesv1.GitOpsSetGenerator{
				Matrix: &templatesv1.MatrixGenerator{
					Generators: []templatesv1.GitOpsSetNestedGenerator{
						{
							Name: "g1",
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"latestImage": "testing:v2.1", "previousImage": "testing:v2.0"}`)},
								},
							},
						},
						{
							Name: "g2",
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"latestImage": "image:v2.1", "previousImage": "image:v2.0"}`)},
								},
							},
						},
						{
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"appName": "test1"}`)},
									{Raw: []byte(`{"appName": "test2"}`)},
									{Raw: []byte(`{"appName": "test3"}`)},
								},
							},
						},
					},
				},
			},
			expectedMatrix: []map[string]any{
				{
					"appName": "test1",
					"g1":      map[string]any{"latestImage": "testing:v2.1", "previousImage": "testing:v2.0"},
					"g2":      map[string]any{"latestImage": "image:v2.1", "previousImage": "image:v2.0"},
				},
				{
					"appName": "test2",
					"g1":      map[string]any{"latestImage": "testing:v2.1", "previousImage": "testing:v2.0"},
					"g2":      map[string]any{"latestImage": "image:v2.1", "previousImage": "image:v2.0"},
				},
				{
					"appName": "test3",
					"g1":      map[string]any{"latestImage": "testing:v2.1", "previousImage": "testing:v2.0"},
					"g2":      map[string]any{"latestImage": "image:v2.1", "previousImage": "image:v2.0"},
				},
			},
		},
		{
			name: "matrix generator in singleElement mode",
			sg: &templatesv1.GitOpsSetGenerator{
				Matrix: &templatesv1.MatrixGenerator{
					SingleElement: true,
					Generators: []templatesv1.GitOpsSetNestedGenerator{
						{
							Name: "list1",
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"key1": "value1"}`)},
									{Raw: []byte(`{"key2": "value2"}`)},
								},
							},
						},
						{
							Name: "list2",
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"key3": "value3"}`)},
									{Raw: []byte(`{"key4": "value4"}`)},
								},
							},
						},
					},
				},
			},
			expectedMatrix: []map[string]any{
				{
					"list1": []map[string]any{{"key1": "value1"}, {"key2": "value2"}},
					"list2": []map[string]any{{"key3": "value3"}, {"key4": "value4"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGenerator(logr.Discard(), newFakeClient(t, tt.objects...), map[string]generators.GeneratorFactory{
				"List":          list.GeneratorFactory,
				"GitRepository": gitrepository.GeneratorFactory(fetch.NewArchiveFetcher(1, tar.UnlimitedUntarSize, tar.UnlimitedUntarSize, "")),
			})
			matrix, err := g.Generate(context.TODO(), tt.sg, tt.ks)
			test.AssertErrorMatch(t, tt.expectedErrorStr, err)

			if diff := cmp.Diff(tt.expectedMatrix, matrix); diff != "" {
				t.Errorf("matrix mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDisabledGenerators(t *testing.T) {
	gen := NewGenerator(logr.Discard(), nil, map[string]generators.GeneratorFactory{
		"List": list.GeneratorFactory,
	})

	sg := &templatesv1.GitOpsSetGenerator{
		Matrix: &templatesv1.MatrixGenerator{
			Generators: []templatesv1.GitOpsSetNestedGenerator{
				{
					List: &templatesv1.ListGenerator{
						Elements: []apiextensionsv1.JSON{
							{Raw: []byte(`{"cluster": "cluster","url": "url"}`)},
						},
					},
				},
				// Not actually used as it is disabled
				{GitRepository: &templatesv1.GitRepositoryGenerator{}},
			},
		},
	}

	ks := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-generator",
			Namespace: testNamespace,
		},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{
				*sg,
			},
		},
	}

	_, err := gen.Generate(context.TODO(), sg, ks)
	test.AssertErrorMatch(t, `generator GitRepository not enabled`, err)
}

func TestInterval(t *testing.T) {
	gen := NewGenerator(logr.Discard(), nil, map[string]generators.GeneratorFactory{
		"List":          list.GeneratorFactory,
		"GitRepository": gitrepository.GeneratorFactory(nil),
		"PullRequests":  pullrequests.GeneratorFactory,
	})

	interval := time.Minute * 30
	sg := &templatesv1.GitOpsSetGenerator{
		Matrix: &templatesv1.MatrixGenerator{
			Generators: []templatesv1.GitOpsSetNestedGenerator{
				{
					List: &templatesv1.ListGenerator{
						Elements: []apiextensionsv1.JSON{
							{Raw: []byte(`{"cluster": "cluster","url": "url"}`)},
						},
					},
					PullRequests: &templatesv1.PullRequestGenerator{
						Driver:    "fake",
						ServerURL: "https://example.com",
						Repo:      "test-org/my-repo",
						Interval:  metav1.Duration{Duration: interval},
					},
				},
			},
		},
	}

	d := gen.Interval(sg)

	if d != interval {
		t.Fatalf("got %#v want %#v", d, interval)
	}
}

func TestSingleElement(t *testing.T) {
	tests := []struct {
		name      string
		generated []generatedElements
		expected  []map[string]any
	}{
		{
			name:      "empty slices",
			generated: []generatedElements{},
			expected:  []map[string]any{},
		},
		{
			name: "both slices have one element",
			generated: []generatedElements{
				{
					name: "staging",
					elements: []map[string]any{
						{"a": 1},
					},
				},
				{
					name: "production",
					elements: []map[string]any{
						{"b": 2},
					},
				},
			},
			expected: []map[string]any{
				{
					"production": []map[string]any{{"b": 2}},
					"staging":    []map[string]any{{"a": 1}},
				},
			},
		},
		{
			name: "multiple unnamed generated sets",
			generated: []generatedElements{
				{
					elements: []map[string]any{
						{"name": "test1", "value": "value1"},
						{"name": "test2", "value": "value2"},
					},
				},
				{
					elements: []map[string]any{
						{"name": "test2", "value": "value3"},
						{"name": "test1", "value": "value4"},
					},
				},
			},
			expected: []map[string]any{
				{
					"Matrix": []map[string]any{
						{"name": "test1", "value": "value1"},
						{"name": "test2", "value": "value2"},
						{"name": "test2", "value": "value3"},
						{"name": "test1", "value": "value4"},
					},
				},
			},
		},
		{
			name: "real-world example",
			generated: []generatedElements{
				{
					name: "staging",
					elements: []map[string]any{
						{"ClusterAnnotations": map[string]string{}, "ClusterLabels": map[string]string{"env": "staging"}, "ClusterName": "staging-cluster1", "ClusterNamespace": "clusters"},
						{"ClusterAnnotations": map[string]string{}, "ClusterLabels": map[string]string{"env": "staging"}, "ClusterName": "staging-cluster2", "ClusterNamespace": "clusters"},
					},
				},
				{
					name: "production",
					elements: []map[string]any{
						{"ClusterAnnotations": map[string]string{}, "ClusterLabels": map[string]string{"env": "production"}, "ClusterName": "production-cluster1", "ClusterNamespace": "clusters"},
						{"ClusterAnnotations": map[string]string{}, "ClusterLabels": map[string]string{"env": "production"}, "ClusterName": "production-cluster2", "ClusterNamespace": "clusters"},
					},
				},
			},
			expected: []map[string]any{
				{
					"production": []map[string]any{
						{
							"ClusterAnnotations": map[string]string{},
							"ClusterLabels": map[string]string{
								"env": "production",
							},
							"ClusterName":      "production-cluster1",
							"ClusterNamespace": "clusters",
						},
						{
							"ClusterAnnotations": map[string]string{},
							"ClusterLabels": map[string]string{
								"env": "production",
							},
							"ClusterName":      "production-cluster2",
							"ClusterNamespace": "clusters",
						},
					},
					"staging": []map[string]any{
						{
							"ClusterAnnotations": map[string]string{},
							"ClusterLabels": map[string]string{
								"env": "staging",
							},
							"ClusterName":      "staging-cluster1",
							"ClusterNamespace": "clusters",
						},
						{
							"ClusterAnnotations": map[string]string{},
							"ClusterLabels": map[string]string{
								"env": "staging",
							},
							"ClusterName":      "staging-cluster2",
							"ClusterNamespace": "clusters",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := singleElement(tt.generated)
			test.AssertNoError(t, err)
			if diff := cmp.Diff(tt.expected, result); diff != "" {
				t.Errorf("singleElement mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func newGitRepository(archiveURL, xsum string) *sourcev1beta2.GitRepository {
	return &sourcev1beta2.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repository",
			Namespace: testNamespace,
		},
		Status: sourcev1beta2.GitRepositoryStatus{
			Artifact: &sourcev1.Artifact{
				URL:    archiveURL,
				Digest: xsum,
			},
		},
	}
}

func newFakeClient(t *testing.T, objs ...runtime.Object) client.WithWatch {
	t.Helper()

	scheme := runtime.NewScheme()

	if err := sourcev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
}
