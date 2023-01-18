package gitrepository

import (
	"context"
	"testing"

	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/test"
)

var _ generators.Generator = (*GitRepositoryGenerator)(nil)

const testNamespace = "generation"

func TestGenerate_with_no_GitRepository(t *testing.T) {
	factory := GeneratorFactory()

	gen := factory(logr.Discard(), nil)
	got, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{}, nil)

	if err != nil {
		t.Errorf("got an error with no GitRepository: %s", err)
	}
	if got != nil {
		t.Errorf("got %v, want %v with no GitRepository generator", got, nil)
	}
}

func TestGenerate(t *testing.T) {
	srv := test.StartFakeArchiveServer(t, "testdata")
	testCases := []struct {
		name      string
		generator *templatesv1.GitRepositoryGenerator
		objects   []runtime.Object
		want      []map[string]any
	}{
		{
			"simple case",
			&templatesv1.GitRepositoryGenerator{
				RepositoryRef: "test-repository",
				Files: []templatesv1.GitRepositoryGeneratorFileItem{
					{Path: "files/dev.yaml"},
					{Path: "files/production.yaml"},
					{Path: "files/staging.yaml"},
				},
			},
			[]runtime.Object{newGitRepository(srv.URL+"/files.tar.gz",
				"f0a57ec1cdebda91cf00d89dfa298c6ac27791e7fdb0329990478061755eaca8")},
			[]map[string]any{
				{"environment": "dev", "instances": 2.0},
				{"environment": "production", "instances": 10.0},
				{"environment": "staging", "instances": 5.0},
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator(logr.Discard(), newFakeClient(t, tt.objects...))
			got, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{
				GitRepository: tt.generator,
			},
				&templatesv1.GitOpsSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-generator",
						Namespace: testNamespace,
					},
					Spec: templatesv1.GitOpsSetSpec{
						Generators: []templatesv1.GitOpsSetGenerator{
							{
								GitRepository: tt.generator,
							},
						},
					},
				})

			test.AssertNoError(t, err)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("failed to generate from git repository:\n%s", diff)
			}
		})
	}
}

func TestInterval(t *testing.T) {
	gen := NewGenerator(logr.Discard(), nil)
	sg := &templatesv1.GitOpsSetGenerator{
		GitRepository: &templatesv1.GitRepositoryGenerator{},
	}

	d := gen.Interval(sg)

	if d != generators.NoRequeueInterval {
		t.Fatalf("got %#v want %#v", d, generators.NoRequeueInterval)
	}
}

func TestGenerate_errors(t *testing.T) {
	factory := GeneratorFactory()
	testCases := []struct {
		name      string
		generator *templatesv1.GitRepositoryGenerator
		objects   []runtime.Object
		wantErr   string
	}{
		{
			name: "missing git repository resource",
			generator: &templatesv1.GitRepositoryGenerator{
				RepositoryRef: "test-repository",
				Files: []templatesv1.GitRepositoryGeneratorFileItem{
					{Path: "files/dev.yaml"},
				},
			},
			wantErr: `could not load GitRepository: gitrepositories.source.toolkit.fluxcd.io "test-repository" not found`,
		},
		{
			name: "generation not configured",
			generator: &templatesv1.GitRepositoryGenerator{
				RepositoryRef: "test-repository",
			},
			wantErr: "GitOpsSet is empty",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gen := factory(logr.Discard(), newFakeClient(t, tt.objects...))
			_, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{
				GitRepository: tt.generator,
			},
				&templatesv1.GitOpsSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-generator",
						Namespace: testNamespace,
					},
					Spec: templatesv1.GitOpsSetSpec{
						Generators: []templatesv1.GitOpsSetGenerator{
							{
								GitRepository: tt.generator,
							},
						},
					},
				})

			test.AssertErrorMatch(t, tt.wantErr, err)
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
				URL:      archiveURL,
				Checksum: xsum,
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
