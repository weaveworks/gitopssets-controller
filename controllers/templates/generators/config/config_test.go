package config

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/gitops-tools/gitopssets-controller/test"
)

func TestGenerate_with_no_Config(t *testing.T) {
	gen := GeneratorFactory(logr.Discard(), nil)
	got, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{}, nil)

	if err != nil {
		t.Errorf("got an error with no Config: %s", err)
	}
	if got != nil {
		t.Errorf("got %v, want %v with no Config generator", got, nil)
	}
}

func TestConfigGenerator_Interval(t *testing.T) {
	gen := NewGenerator(logr.Discard(), nil)
	sg := &templatesv1.GitOpsSetGenerator{
		Config: &templatesv1.ConfigGenerator{},
	}

	d := gen.Interval(sg)

	if d != generators.NoRequeueInterval {
		t.Fatalf("got %#v want %#v", d, generators.NoRequeueInterval)
	}
}

func TestConfigGenerator_Generate_with_errors(t *testing.T) {
	tests := []struct {
		name    string
		sg      *templatesv1.GitOpsSetGenerator
		objects []runtime.Object
		wantErr string
	}{
		{
			name: "generator referencing non-existent ConfigMap",
			sg: &templatesv1.GitOpsSetGenerator{
				Config: &templatesv1.ConfigGenerator{
					Kind: "ConfigMap",
					Name: "test-config-map",
				},
			},
			wantErr: `configmaps "test-config-map" not found`,
		},
		{
			name: "generator referencing non-existent Secret",
			sg: &templatesv1.GitOpsSetGenerator{
				Config: &templatesv1.ConfigGenerator{
					Kind: "Secret",
					Name: "test-secret",
				},
			},
			wantErr: `secrets "test-secret" not found`,
		},
		{
			name: "generator referencing unknown Kind",
			sg: &templatesv1.GitOpsSetGenerator{
				Config: &templatesv1.ConfigGenerator{
					Kind: "Database",
					Name: "test-database",
				},
			},
			wantErr: `unknown Config Kind "Database" "test-database"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newFakeClient(t, tt.objects...)
			g := NewGenerator(logr.Discard(), c)

			_, err := g.Generate(context.TODO(), tt.sg, &templatesv1.GitOpsSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "demo-set",
					Namespace: "testing",
				},
			})

			test.AssertErrorMatch(t, tt.wantErr, err)
		})
	}
}

func TestConfigGenerator_Generate(t *testing.T) {
	tests := []struct {
		name    string
		sg      *templatesv1.GitOpsSetGenerator
		objects []runtime.Object
		want    []map[string]any
	}{
		{
			name: "generator referencing a ConfigMap",
			sg: &templatesv1.GitOpsSetGenerator{
				Config: &templatesv1.ConfigGenerator{
					Kind: "ConfigMap",
					Name: "test-config-map",
				},
			},
			objects: []runtime.Object{
				test.NewConfigMap(func(cm *corev1.ConfigMap) {
					cm.ObjectMeta.Name = "test-config-map"
					cm.ObjectMeta.Namespace = "testing"
					cm.Data = map[string]string{
						"test-key1": "test-value1",
						"test-key2": "test-value2",
					}
				}),
			},
			want: []map[string]any{
				{
					"test-key1": "test-value1",
					"test-key2": "test-value2",
				},
			},
		},
		{
			name: "generator referencing a Secret",
			sg: &templatesv1.GitOpsSetGenerator{
				Config: &templatesv1.ConfigGenerator{
					Kind: "Secret",
					Name: "test-config-secret",
				},
			},
			objects: []runtime.Object{
				test.NewSecret(func(cm *corev1.Secret) {
					cm.ObjectMeta.Name = "test-config-secret"
					cm.ObjectMeta.Namespace = "testing"
					cm.Data = map[string][]byte{
						"test-key1": []byte("test-value1"),
						"test-key2": []byte("test-value2"),
					}
				}),
			},
			want: []map[string]any{
				{
					"test-key1": "test-value1",
					"test-key2": "test-value2",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newFakeClient(t, tt.objects...)
			g := NewGenerator(logr.Discard(), c)

			params, err := g.Generate(context.TODO(), tt.sg, &templatesv1.GitOpsSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "demo-set",
					Namespace: "testing",
				},
			})

			test.AssertNoError(t, err)
			if diff := cmp.Diff(tt.want, params); diff != "" {
				t.Fatalf("failed to generate params:\n%s", diff)
			}
		})
	}
}

func newFakeClient(t *testing.T, objs ...runtime.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	assert.NoError(t, templatesv1.AddToScheme(scheme))
	assert.NoError(t, clientgoscheme.AddToScheme(scheme))

	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
}
