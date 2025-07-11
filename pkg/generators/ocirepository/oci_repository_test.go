package ocirepository

import (
	"context"
	"testing"

	"github.com/fluxcd/pkg/http/fetch"
	"github.com/fluxcd/pkg/tar"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/pkg/generators"
	"github.com/weaveworks/gitopssets-controller/test"
)

const testRetries int = 3

var _ generators.Generator = (*OCIRepositoryGenerator)(nil)

var testFetcher = fetch.NewArchiveFetcher(testRetries, tar.UnlimitedUntarSize, tar.UnlimitedUntarSize, "")

func TestGenerate_with_no_OCIRepository(t *testing.T) {
	gen := GeneratorFactory(testFetcher)(logr.Discard(), nil)
	got, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{}, nil)

	if err != nil {
		t.Errorf("got an error with no OCIRepository: %s", err)
	}
	if got != nil {
		t.Errorf("got %v, want %v with no OCIRepository generator", got, nil)
	}
}

func TestGenerate(t *testing.T) {
	srv := test.StartFakeArchiveServer(t, "testdata")
	testCases := []struct {
		name      string
		generator *templatesv1.OCIRepositoryGenerator
		objects   []runtime.Object
		want      []map[string]any
	}{
		{
			"file list case",
			&templatesv1.OCIRepositoryGenerator{
				RepositoryRef: "test-repository",
				Files: []templatesv1.RepositoryGeneratorFileItem{
					{Path: "files/dev.yaml"},
					{Path: "files/production.yaml"},
					{Path: "files/staging.yaml"},
				},
			},
			[]runtime.Object{newOCIRepository(
				withArchiveURLAndChecksum(srv.URL+"/files.tar.gz",
					"sha256:f0a57ec1cdebda91cf00d89dfa298c6ac27791e7fdb0329990478061755eaca8"))},
			[]map[string]any{
				{"environment": "dev", "instances": 2.0},
				{"environment": "production", "instances": 10.0},
				{"environment": "staging", "instances": 5.0},
			},
		},
		{
			"directory generation",
			&templatesv1.OCIRepositoryGenerator{
				RepositoryRef: "test-repository",
				Directories: []templatesv1.RepositoryGeneratorDirectoryItem{
					{Path: "applications/*"},
				},
			},
			[]runtime.Object{newOCIRepository(
				withArchiveURLAndChecksum(srv.URL+"/directories.tar.gz",
					"sha256:a8bb41d733c5cc9bdd13d926a2edbe4c85d493c6c90271da1e1b991880935dc1"))},
			[]map[string]any{
				{"Directory": "./applications/backend", "Base": "backend"},
				{"Directory": "./applications/frontend", "Base": "frontend"},
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator(logr.Discard(), newFakeClient(t, tt.objects...), testFetcher)
			got, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{
				OCIRepository: tt.generator,
			},
				&templatesv1.GitOpsSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-generator",
						Namespace: "default",
					},
					Spec: templatesv1.GitOpsSetSpec{
						Generators: []templatesv1.GitOpsSetGenerator{
							{
								OCIRepository: tt.generator,
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
	gen := NewGenerator(logr.Discard(), nil, nil)
	sg := &templatesv1.GitOpsSetGenerator{
		OCIRepository: &templatesv1.OCIRepositoryGenerator{},
	}

	d := gen.Interval(sg)

	if d != generators.NoRequeueInterval {
		t.Fatalf("got %#v want %#v", d, generators.NoRequeueInterval)
	}
}

func TestGenerate_errors(t *testing.T) {
	testCases := []struct {
		name      string
		generator *templatesv1.OCIRepositoryGenerator
		objects   []runtime.Object
		wantErr   string
	}{
		{
			name: "missing git repository resource",
			generator: &templatesv1.OCIRepositoryGenerator{
				RepositoryRef: "test-repository",
				Files: []templatesv1.RepositoryGeneratorFileItem{
					{Path: "files/dev.yaml"},
				},
			},
			wantErr: `could not load OCIRepository: ocirepositories.source.toolkit.fluxcd.io "test-repository" not found`,
		},
		{
			name: "generation not configured",
			generator: &templatesv1.OCIRepositoryGenerator{
				RepositoryRef: "test-repository",
			},
			wantErr: "GitOpsSet is empty",
		},
		{
			name: "no artifact in OCIRepository",
			generator: &templatesv1.OCIRepositoryGenerator{
				RepositoryRef: "test-repository",
				Files: []templatesv1.RepositoryGeneratorFileItem{
					{Path: "files/dev.yaml"},
					{Path: "files/production.yaml"},
					{Path: "files/staging.yaml"},
				},
			},
			objects: []runtime.Object{newOCIRepository()},
			wantErr: "no artifact for OCIRepository default/test-repository",
		},
		{
			name: "no artifact in OCIRepository with dirs",
			generator: &templatesv1.OCIRepositoryGenerator{
				RepositoryRef: "test-repository",
				Directories: []templatesv1.RepositoryGeneratorDirectoryItem{
					{Path: "files/*"},
				},
			},
			objects: []runtime.Object{newOCIRepository()},
			wantErr: "no artifact for OCIRepository default/test-repository",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gen := GeneratorFactory(testFetcher)(logr.Discard(), newFakeClient(t, tt.objects...))
			_, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{
				OCIRepository: tt.generator,
			},
				&templatesv1.GitOpsSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-generator",
						Namespace: "default",
					},
					Spec: templatesv1.GitOpsSetSpec{
						Generators: []templatesv1.GitOpsSetGenerator{
							{
								OCIRepository: tt.generator,
							},
						},
					},
				})

			test.AssertErrorMatch(t, tt.wantErr, err)
		})
	}
}

func withArchiveURLAndChecksum(archiveURL, xsum string) func(*sourcev1beta2.OCIRepository) {
	return func(gr *sourcev1beta2.OCIRepository) {
		gr.Status.Artifact = &sourcev1.Artifact{
			URL:    archiveURL,
			Digest: xsum,
		}
	}
}

func newOCIRepository(opts ...func(*sourcev1beta2.OCIRepository)) *sourcev1beta2.OCIRepository {
	gr := &sourcev1beta2.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repository",
			Namespace: "default",
		},
	}

	for _, opt := range opts {
		opt(gr)
	}

	return gr
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
