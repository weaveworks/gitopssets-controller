package matrix

import (
	"context"
	"testing"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/gitrepository"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/list"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/pullrequests"
	"github.com/weaveworks/gitopssets-controller/test"
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
		Files: []templatesv1.GitRepositoryGeneratorFileItem{
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
			ks:               nil,
			expectedMatrix:   nil,
			expectedErrorStr: "matrix generator needs two generators",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGenerator(logr.Discard(), newFakeClient(t, tt.objects...), map[string]generators.GeneratorFactory{
				"List":          list.GeneratorFactory,
				"GitRepository": gitrepository.GeneratorFactory,
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
		"GitRepository": gitrepository.GeneratorFactory,
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

func TestCartesian(t *testing.T) {
	tests := []struct {
		name     string
		slice1   []map[string]any
		slice2   []map[string]any
		expected []map[string]any
	}{
		{
			name:     "empty slices",
			slice1:   []map[string]any{},
			slice2:   []map[string]any{},
			expected: nil,
		},
		{
			name: "one empty slice",
			slice1: []map[string]any{
				{"a": 1},
				{"a": 2},
			},
			slice2:   []map[string]any{},
			expected: nil,
		},
		{
			name: "both slices have one element",
			slice1: []map[string]any{
				{"a": 1},
			},
			slice2: []map[string]any{
				{"b": 2},
			},
			expected: []map[string]any{
				{"a": 1, "b": 2},
			},
		},
		{
			name: "both slices have multiple elements",
			slice1: []map[string]any{
				{"a": 1},
				{"a": 2},
			},
			slice2: []map[string]any{
				{"b": 3},
				{"b": 4},
			},
			expected: []map[string]any{
				{"a": 1, "b": 3},
				{"a": 1, "b": 4},
				{"a": 2, "b": 3},
				{"a": 2, "b": 4},
			},
		},
		{
			name: "overlapping values and different ordering",
			slice1: []map[string]any{
				{"name": "test1", "value": "value1"},
				{"name": "test2", "value": "value2"},
			},
			slice2: []map[string]any{
				{"name": "test2", "value": "value3"},
				{"name": "test1", "value": "value4"},
			},
			expected: []map[string]any{
				{"name": "test2", "value": "value3"},
				{"name": "test1", "value": "value4"},
			},
		},
		{
			name: "nested maps",
			slice1: []map[string]any{
				{"a": 1, "b": map[string]any{"c": 2, "d": 3}},
				{"a": 4, "b": map[string]any{"c": 5, "d": 6}},
			},
			slice2: []map[string]any{
				{"e": 7, "f": map[string]any{"g": 8, "h": 9}},
				{"e": 10, "f": map[string]any{"g": 11, "h": 12}},
			},
			expected: []map[string]any{
				{"a": 1, "b": map[string]any{"c": 2, "d": 3}, "e": 7, "f": map[string]any{"g": 8, "h": 9}},
				{"a": 1, "b": map[string]any{"c": 2, "d": 3}, "e": 10, "f": map[string]any{"g": 11, "h": 12}},
				{"a": 4, "b": map[string]any{"c": 5, "d": 6}, "e": 7, "f": map[string]any{"g": 8, "h": 9}},
				{"a": 4, "b": map[string]any{"c": 5, "d": 6}, "e": 10, "f": map[string]any{"g": 11, "h": 12}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := cartesian(tt.slice1, tt.slice2)
			test.AssertNoError(t, err)
			if diff := cmp.Diff(tt.expected, result); diff != "" {
				t.Errorf("cartesian mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func newGitRepository(archiveURL, xsum string) *sourcev1.GitRepository {
	return &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repository",
			Namespace: testNamespace,
		},
		Status: sourcev1.GitRepositoryStatus{
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

	if err := sourcev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
}
