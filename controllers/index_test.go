package controllers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

func TestIndexesIncludeMatrixGenerators(t *testing.T) {
	set := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "demo"},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{{
				GitRepository: &templatesv1.GitRepositoryGenerator{RepositoryRef: "app"},
				Matrix: &templatesv1.MatrixGenerator{
					Generators: []templatesv1.GitOpsSetNestedGenerator{
						{GitRepository: &templatesv1.GitRepositoryGenerator{RepositoryRef: "platform"}},
						{OCIRepository: &templatesv1.OCIRepositoryGenerator{RepositoryRef: "charts"}},
						{Config: &templatesv1.ConfigGenerator{Kind: "Secret", Name: "creds"}},
						{Config: &templatesv1.ConfigGenerator{Kind: "ConfigMap", Name: "settings"}},
						{ImagePolicy: &templatesv1.ImagePolicyGenerator{PolicyRef: "app-policy"}},
					},
				},
			}},
		},
	}

	assertKeys(t, indexGitRepositories(set), "demo/app", "demo/platform")
	assertKeys(t, indexOCIRepositories(set), "demo/charts")
	assertKeys(t, indexConfig("Secret")(set), "demo/creds")
	assertKeys(t, indexConfig("ConfigMap")(set), "demo/settings")
	assertKeys(t, indexImagePolicies(set), "demo/app-policy")
	if indexGitRepositories(&templatesv1.GitOpsSet{}) != nil {
		t.Fatal("empty set indexed a git repository")
	}
}

func TestIndexedWatchesEnqueueMatchingSets(t *testing.T) {
	set := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "demo"},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{{
				GitRepository: &templatesv1.GitRepositoryGenerator{RepositoryRef: "app"},
				OCIRepository: &templatesv1.OCIRepositoryGenerator{RepositoryRef: "charts"},
				ImagePolicy:   &templatesv1.ImagePolicyGenerator{PolicyRef: "app-policy"},
				Config:        &templatesv1.ConfigGenerator{Kind: "ConfigMap", Name: "settings"},
			}},
		},
	}
	sch := runtime.NewScheme()
	if err := scheme.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	if err := sourcev1.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	if err := imagev1.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithIndex(&templatesv1.GitOpsSet{}, gitRepositoryIndexKey, indexGitRepositories).
		WithIndex(&templatesv1.GitOpsSet{}, ociRepositoryIndexKey, indexOCIRepositories).
		WithIndex(&templatesv1.GitOpsSet{}, imagePolicyIndexKey, indexImagePolicies).
		WithIndex(&templatesv1.GitOpsSet{}, configMapIndexKey, indexConfig("ConfigMap")).
		WithIndex(&templatesv1.GitOpsSet{}, secretIndexKey, indexConfig("Secret")).
		WithRuntimeObjects(set).
		Build()
	reconciler := &GitOpsSetReconciler{Client: cl}

	cases := []struct {
		name string
		obj  client.Object
		mapf func(client.Object) []reconcile.Request
	}{
		{"git", &sourcev1.GitRepository{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "demo"}}, func(obj client.Object) []reconcile.Request {
			return reconciler.gitRepositoryToGitOpsSet(t.Context(), obj)
		}},
		{"oci", &sourcev1.OCIRepository{ObjectMeta: metav1.ObjectMeta{Name: "charts", Namespace: "demo"}}, func(obj client.Object) []reconcile.Request {
			return reconciler.ociRepositoryToGitOpsSet(t.Context(), obj)
		}},
		{"image", &imagev1.ImagePolicy{ObjectMeta: metav1.ObjectMeta{Name: "app-policy", Namespace: "demo"}}, func(obj client.Object) []reconcile.Request {
			return reconciler.imagePolicyToGitOpsSet(t.Context(), obj)
		}},
		{"configmap", &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "demo"}}, func(obj client.Object) []reconcile.Request {
			return reconciler.configMapToGitOpsSet(t.Context(), obj)
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.mapf(tt.obj)
			if len(got) != 1 || got[0].Name != "set" || got[0].Namespace != "demo" {
				t.Fatalf("requests = %#v", got)
			}
		})
	}

	if got := reconciler.secretToGitOpsSet(t.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "missing", Namespace: "demo"}}); len(got) != 0 {
		t.Fatalf("unexpected secret requests %#v", got)
	}
}

func TestIndexesPanicOnUnexpectedType(t *testing.T) {
	pod := &corev1.Pod{}
	for _, fn := range []func(client.Object) []string{
		indexClusterGenerators,
		indexGitRepositories,
		indexOCIRepositories,
		indexImagePolicies,
		indexConfig("Secret"),
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			fn(pod)
		}()
	}
}

func TestMakeImpersonationClient(t *testing.T) {
	reconciler := &GitOpsSetReconciler{
		Config: &rest.Config{Host: "https://127.0.0.1:1"},
		Scheme: runtime.NewScheme(),
	}
	got, err := reconciler.makeImpersonationClient("demo", "builder")
	if err != nil || got == nil {
		t.Fatalf("client = %v, err = %v", got, err)
	}
}

func assertKeys(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %v, want %v", got, want)
		}
	}
}
