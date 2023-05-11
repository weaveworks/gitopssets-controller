package imagepolicy

import (
	"context"
	"testing"

	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
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

var _ generators.Generator = (*ImagePolicyGenerator)(nil)

func TestGenerate_with_no_ImagePolicy(t *testing.T) {
	gen := GeneratorFactory(logr.Discard(), nil)
	got, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{}, nil)

	if err != nil {
		t.Errorf("got an error with no ImagePolicy: %s", err)
	}
	if got != nil {
		t.Errorf("got %v, want %v with no ImagePolicy generator", got, nil)
	}
}

func TestGenerate(t *testing.T) {
	testCases := []struct {
		name      string
		generator *templatesv1.ImagePolicyGenerator
		objects   []runtime.Object
		want      []map[string]any
	}{
		{
			"no policy image",
			&templatesv1.ImagePolicyGenerator{
				PolicyRef: "test-policy",
			},
			[]runtime.Object{test.NewImagePolicy()},
			[]map[string]any{},
		},
		{
			"image policy in status",
			&templatesv1.ImagePolicyGenerator{
				PolicyRef: "test-policy",
			},
			[]runtime.Object{test.NewImagePolicy(withImages("testing/test:v0.30.0", "testing/test:v0.29.0"))},
			[]map[string]any{
				{
					"latestImage":   "testing/test:v0.30.0",
					"previousImage": "testing/test:v0.29.0",
				},
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator(logr.Discard(), newFakeClient(t, tt.objects...))
			got, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{
				ImagePolicy: tt.generator,
			},
				&templatesv1.GitOpsSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-generator",
						Namespace: "default",
					},
					Spec: templatesv1.GitOpsSetSpec{
						Generators: []templatesv1.GitOpsSetGenerator{
							{
								ImagePolicy: tt.generator,
							},
						},
					},
				})

			test.AssertNoError(t, err)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("failed to generate from git policy:\n%s", diff)
			}
		})
	}
}

func TestInterval(t *testing.T) {
	gen := NewGenerator(logr.Discard(), nil)
	sg := &templatesv1.GitOpsSetGenerator{
		ImagePolicy: &templatesv1.ImagePolicyGenerator{},
	}

	d := gen.Interval(sg)

	if d != generators.NoRequeueInterval {
		t.Fatalf("got %#v want %#v", d, generators.NoRequeueInterval)
	}
}

func TestGenerate_errors(t *testing.T) {
	testCases := []struct {
		name      string
		generator *templatesv1.ImagePolicyGenerator
		objects   []runtime.Object
		wantErr   string
	}{
		{
			name: "missing image policy resource",
			generator: &templatesv1.ImagePolicyGenerator{
				PolicyRef: "test-policy",
			},
			wantErr: `could not load ImagePolicy: imagepolicies.image.toolkit.fluxcd.io "test-policy" not found`,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gen := GeneratorFactory(logr.Discard(), newFakeClient(t, tt.objects...))
			_, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{
				ImagePolicy: tt.generator,
			},
				&templatesv1.GitOpsSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-generator",
						Namespace: "default",
					},
					Spec: templatesv1.GitOpsSetSpec{
						Generators: []templatesv1.GitOpsSetGenerator{
							{
								ImagePolicy: tt.generator,
							},
						},
					},
				})

			test.AssertErrorMatch(t, tt.wantErr, err)
		})
	}
}

func withImages(latestImage, previousImage string) func(*imagev1.ImagePolicy) {
	return func(ip *imagev1.ImagePolicy) {
		ip.Status.LatestImage = latestImage
		ip.Status.ObservedPreviousImage = previousImage
	}
}

func newFakeClient(t *testing.T, objs ...runtime.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := imagev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
}