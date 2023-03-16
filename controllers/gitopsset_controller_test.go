package controllers

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
	"time"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta2"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/google/go-cmp/cmp"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators/list"
	"github.com/weaveworks/gitopssets-controller/test"
)

var kustomizationGVK = schema.GroupVersionKind{
	Group:   "kustomize.toolkit.fluxcd.io",
	Kind:    "Kustomization",
	Version: "v1beta2",
}

func TestEmptyCases(t *testing.T) {
	testEnv := &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join("..", "config", "crd", "bases"),
			"testdata/crds",
		},
	}
	testEnv.ControlPlane.GetAPIServer().Configure().Append("--authorization-mode=RBAC")

	cfg, err := testEnv.Start()
	test.AssertNoError(t, err)
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("failed to stop the test environment: %s", err)
		}
	}()

	scheme := runtime.NewScheme()
	// This deliberately only sets up the scheme for the core scheme + the
	// GitOpsSets templating scheme.
	// All other resources must be created via unstructureds, this includes
	// Kustomizations.
	test.AssertNoError(t, clientgoscheme.AddToScheme(scheme))
	test.AssertNoError(t, templatesv1.AddToScheme(scheme))

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	test.AssertNoError(t, err)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme})
	test.AssertNoError(t, err)

	reconciler := &GitOpsSetReconciler{
		Client:                k8sClient,
		Scheme:                scheme,
		DefaultServiceAccount: "",
		Generators: map[string]generators.GeneratorFactory{
			"List": list.GeneratorFactory,
		},
		Config: cfg,
	}

	test.AssertNoError(t, reconciler.SetupWithManager(mgr))

	t.Run("Reconciling when creation is suspended", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Suspend = true
		})
		test.AssertNoError(t, k8sClient.Create(ctx, gs))
		defer deleteGitOpsSetAndFinalize(t, k8sClient, gs, reconciler)
		reconcileAndAssertFinalizerExists(t, k8sClient, reconciler, gs)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		updated := &templatesv1.GitOpsSet{}
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated))

		assertInventoryHasNoItems(t, updated)
		assertNoKustomizationsExistInNamespace(t, k8sClient, "default")
	})

	t.Run("Reconciling with no generated resources", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			// No templates to generate resources from
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{}
			gs.Spec.Generators = []templatesv1.GitOpsSetGenerator{
				{
					List: &templatesv1.ListGenerator{
						Elements: []apiextensionsv1.JSON{
							{Raw: []byte(`{"cluster": "engineering-dev"}`)},
						},
					},
				},
			}
		})
		test.AssertNoError(t, k8sClient.Create(ctx, gs))
		defer deleteGitOpsSetAndFinalize(t, k8sClient, gs, reconciler)
		reconcileAndAssertFinalizerExists(t, k8sClient, reconciler, gs)

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		if err != nil {
			t.Fatal(err)
		}

		updated := &templatesv1.GitOpsSet{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated); err != nil {
			t.Fatal(err)
		}

		assertInventoryHasNoItems(t, updated)
	})

	t.Run("Reconcile deletion with no prune", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Prune = false
		})
		test.AssertNoError(t, k8sClient.Create(ctx, gs))
		reconcileAndAssertFinalizerExists(t, k8sClient, reconciler, gs)

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		if err != nil {
			t.Fatal(err)
		}

		updated := &templatesv1.GitOpsSet{}
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated))
		assertGitOpsSetCondition(t, updated, meta.ReadyCondition, "3 resources created")

		deleteGitOpsSetAndFinalize(t, k8sClient, gs, reconciler)

		assertKustomizationsExist(t, k8sClient, "default", "engineering-dev-demo", "engineering-prod-demo", "engineering-preprod-demo")
		if !updated.ObjectMeta.DeletionTimestamp.IsZero() {
			t.Error("GitOpsSet is not marked as deleted")
		}
	})
}

// this runs a single reconciliation and asserts that the set finalizer is
// applied/
// This is needed because the reconciler returns after applying the finalizer to
// avoid race conditions.
func reconcileAndAssertFinalizerExists(t *testing.T, cl client.Client, reconciler *GitOpsSetReconciler, gs *templatesv1.GitOpsSet) {
	ctx := context.TODO()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
	if err != nil {
		t.Fatal(err)
	}

	updated := &templatesv1.GitOpsSet{}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(gs), updated); err != nil {
		t.Fatal(err)
	}

	if !controllerutil.ContainsFinalizer(updated, templatesv1.GitOpsSetFinalizer) {
		t.Fatal("GitOpsSet is missing the finalizer")
	}
}

func assertKustomizationsExist(t *testing.T, cl client.Client, ns string, want ...string) {
	t.Helper()
	gss := &unstructured.UnstructuredList{}
	gss.SetGroupVersionKind(kustomizationGVK)
	test.AssertNoError(t, cl.List(context.TODO(), gss, client.InNamespace(ns)))

	existingNames := func(l []unstructured.Unstructured) []string {
		names := []string{}
		for _, v := range l {
			names = append(names, v.GetName())
		}
		sort.Strings(names)
		return names
	}(gss.Items)

	sort.Strings(want)
	if diff := cmp.Diff(want, existingNames); diff != "" {
		t.Fatalf("got different names:\n%s", diff)
	}
}

func assertNoKustomizationsExistInNamespace(t *testing.T, cl client.Client, ns string) {
	t.Helper()
	gss := &unstructured.UnstructuredList{}
	gss.SetGroupVersionKind(kustomizationGVK)
	test.AssertNoError(t, cl.List(context.TODO(), gss, client.InNamespace(ns)))

	if len(gss.Items) != 0 {
		t.Fatalf("want no Kustomizations to exist, got %v", len(gss.Items))
	}
}

func assertGitOpsSetCondition(t *testing.T, gs *templatesv1.GitOpsSet, condType, msg string) {
	t.Helper()
	cond := apimeta.FindStatusCondition(gs.Status.Conditions, condType)
	if cond == nil {
		t.Fatalf("failed to find matching status condition for type %s in %#v", condType, gs.Status.Conditions)
	}
	if cond.Message != msg {
		t.Fatalf("got %s, want %s", cond.Message, msg)
	}
}

func assertInventoryHasNoItems(t *testing.T, gs *templatesv1.GitOpsSet) {
	t.Helper()
	if gs.Status.Inventory == nil {
		return
	}

	if l := len(gs.Status.Inventory.Entries); l != 0 {
		t.Errorf("expected inventory to have 0 items, got %v", l)
	}
}

func deleteGitOpsSetAndFinalize(t *testing.T, cl client.Client, gs *templatesv1.GitOpsSet, reconciler *GitOpsSetReconciler) {
	t.Helper()
	ctx := context.TODO()
	if err := cl.Delete(ctx, gs); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)}); err != nil {
		t.Fatal(err)
	}
}

func makeTestGitOpsSet(t *testing.T, opts ...func(*templatesv1.GitOpsSet)) *templatesv1.GitOpsSet {
	t.Helper()
	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-set",
			Namespace: "default",
		},
		Spec: templatesv1.GitOpsSetSpec{
			Prune: true,
			Generators: []templatesv1.GitOpsSetGenerator{
				{
					List: &templatesv1.ListGenerator{
						Elements: []apiextensionsv1.JSON{
							{Raw: []byte(`{"cluster": "engineering-dev"}`)},
							{Raw: []byte(`{"cluster": "engineering-prod"}`)},
							{Raw: []byte(`{"cluster": "engineering-preprod"}`)},
						},
					},
				},
			},
			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("default", "{{ .Element.cluster }}-demo"), func(k *kustomizev1.Kustomization) {
							k.Spec = kustomizev1.KustomizationSpec{
								Interval: metav1.Duration{Duration: 5 * time.Minute},
								Path:     "./clusters/{{ .Element.cluster }}/",
								Prune:    true,
								SourceRef: kustomizev1.CrossNamespaceSourceReference{
									Kind: "GitRepository",
									Name: "demo-repo",
								},
								KubeConfig: &meta.KubeConfigReference{
									SecretRef: meta.SecretKeyReference{
										Name: "{{ .Element.cluster }}",
									},
								},
							}
						})),
					},
				},
			},
		},
	}

	for _, o := range opts {
		o(gs)
	}

	return gs
}

func mustMarshalJSON(t *testing.T, r runtime.Object) []byte {
	b, err := json.Marshal(r)
	test.AssertNoError(t, err)

	return b
}

func nsn(namespace, name string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

func deleteObject(t *testing.T, cl client.Client, obj client.Object) {
	t.Helper()
	if err := cl.Delete(context.TODO(), obj); err != nil {
		t.Fatal(err)
	}
}
