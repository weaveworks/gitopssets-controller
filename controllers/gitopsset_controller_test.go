package controllers

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta2"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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

func TestReconciliation(t *testing.T) {
	testEnv := &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join("..", "config", "crd", "bases"),
			"testdata/crds",
		},
	}

	cfg, err := testEnv.Start()
	test.AssertNoError(t, err)

	scheme := runtime.NewScheme()
	test.AssertNoError(t, clientgoscheme.AddToScheme(scheme))
	test.AssertNoError(t, templatesv1.AddToScheme(scheme))

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	test.AssertNoError(t, err)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme})
	test.AssertNoError(t, err)

	reconciler := &GitOpsSetReconciler{
		Client: k8sClient,
		Scheme: scheme,
		Generators: map[string]generators.GeneratorFactory{
			"List": list.GeneratorFactory(),
		},
	}

	test.AssertNoError(t, reconciler.SetupWithManager(mgr))

	t.Run("reconciling creation of new resources", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t)
		test.AssertNoError(t, k8sClient.Create(ctx, gs))

		defer cleanupResource(t, k8sClient, gs)
		defer deleteAllKustomizations(t, k8sClient)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		updated := &templatesv1.GitOpsSet{}
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated))

		want := []runtime.Object{
			makeTestKustomization(nsn("default", "engineering-dev-demo")),
			makeTestKustomization(nsn("default", "engineering-prod-demo")),
			makeTestKustomization(nsn("default", "engineering-preprod-demo")),
		}
		assertInventoryHasItems(t, updated, want...)
		assertGitOpsSetCondition(t, updated, meta.ReadyCondition, "3 resources created")
		assertKustomizationsExist(t, k8sClient, "default", "engineering-dev-demo", "engineering-prod-demo", "engineering-preprod-demo")
	})

	t.Run("reconciling removal of resources", func(t *testing.T) {
		ctx := context.TODO()
		devKS := makeTestKustomization(nsn("default", "engineering-dev-demo"))
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Generators = []templatesv1.GitOpsSetGenerator{
				{
					List: &templatesv1.ListGenerator{
						Elements: []apiextensionsv1.JSON{
							{Raw: []byte(`{"cluster": "engineering-prod"}`)},
							{Raw: []byte(`{"cluster": "engineering-preprod"}`)},
						},
					},
				},
			}
		})

		test.AssertNoError(t, k8sClient.Create(ctx, gs))
		defer cleanupResource(t, k8sClient, gs)

		test.AssertNoError(t, k8sClient.Create(ctx, test.ToUnstructured(t, devKS)))
		defer deleteAllKustomizations(t, k8sClient)

		ref, err := resourceRefFromObject(devKS)
		test.AssertNoError(t, err)

		gs.Status.Inventory = &templatesv1.ResourceInventory{
			Entries: []templatesv1.ResourceRef{ref},
		}
		test.AssertNoError(t, k8sClient.Status().Update(ctx, gs))

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		updated := &templatesv1.GitOpsSet{}
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated))
		want := []runtime.Object{
			makeTestKustomization(nsn("default", "engineering-prod-demo")),
			makeTestKustomization(nsn("default", "engineering-preprod-demo")),
		}
		assertInventoryHasItems(t, updated, want...)
		assertResourceDoesNotExist(t, k8sClient, devKS)
	})

	// t.Run("reconciling update of resources", func(t *testing.T) {
	// 	ctx := context.TODO()
	// 	devKS := makeTestKustomization(nsn("engineering-dev-demo", "default"), func(k *kustomizev1.Kustomization) {
	// 		k.ObjectMeta.Annotations = map[string]string{
	// 			"testing": "testing",
	// 		}
	// 	})
	// 	gs := makeTestGitOpsSet(func(gs *templatesv1.GitOpsSet) {
	// 		gs.Spec.Template.KustomizationSetTemplateMeta = templatesv1.GitOpsSetTemplateMeta{
	// 			Name:      `{{.cluster}}-demo`,
	// 			Namespace: "default",
	// 			Annotations: map[string]string{
	// 				"testing.cluster": "{{.cluster}}",
	// 			},
	// 		}
	// 		gs.Spec.Generators = []templatesv1.GitOpsSetGenerator{
	// 			{
	// 				List: &templatesv1.ListGenerator{
	// 					Elements: []apiextensionsv1.JSON{
	// 						{Raw: []byte(`{"cluster": "engineering-dev"}`)},
	// 					},
	// 				},
	// 			},
	// 		}
	// 	})
	// 	// TODO: create and cleanup
	// 	if err := k8sClient.Create(ctx, gs); err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	defer cleanupResource(t, k8sClient, gs)
	// 	if err := k8sClient.Create(ctx, devKS); err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	defer deleteAllKustomizations(t, k8sClient)

	// 	objMeta, err := object.RuntimeToObjMeta(devKS)
	// 	if err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	gs.Status.Inventory = &templatesv1.ResourceInventory{
	// 		Entries: []templatesv1.ResourceRef{
	// 			{
	// 				ID:      objMeta.String(),
	// 				Version: devKS.GetObjectKind().GroupVersionKind().GroupVersion().String(),
	// 			},
	// 		},
	// 	}
	// 	if err := k8sClient.Status().Update(ctx, gs); err != nil {
	// 		t.Fatal(err)
	// 	}

	// 	_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
	// 	if err != nil {
	// 		t.Fatal(err)
	// 	}

	// 	updated := &templatesv1.GitOpsSet{}
	// 	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated); err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	wantUpdated := makeTestKustomization("engineering-dev-demo", "default", func(k *kustomizev1.Kustomization) {
	// 		k.ObjectMeta.Annotations = map[string]string{
	// 			"testing.cluster": "engineering-dev",
	// 		}
	// 		k.Spec.Path = "./clusters/engineering-dev/"
	// 		k.Spec.KubeConfig = &kustomizev1.KubeConfig{SecretRef: meta.SecretKeyReference{Name: "engineering-dev"}}
	// 	})
	// 	want := []runtime.Object{
	// 		wantUpdated,
	// 	}
	// 	assertInventoryHasItems(t, updated, want...)
	// 	var kustomization kustomizev1.Kustomization
	// 	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(wantUpdated), &kustomization); err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	if diff := cmp.Diff(wantUpdated, &kustomization, objectMetaIgnore()...); diff != "" {
	// 		t.Fatalf("failed to update Kustomization:\n%s", diff)
	// 	}
	// })
}

func deleteAllKustomizations(t *testing.T, cl client.Client) {
	t.Helper()
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kustomizationGVK)

	err := cl.DeleteAllOf(context.TODO(), u, client.InNamespace("default"))
	if client.IgnoreNotFound(err) != nil {
		t.Fatal(err)
	}
}

func assertResourceDoesNotExist(t *testing.T, cl client.Client, gs *kustomizev1.Kustomization) {
	t.Helper()
	check := &unstructured.Unstructured{}
	check.SetGroupVersionKind(kustomizationGVK)

	if err := cl.Get(context.TODO(), client.ObjectKeyFromObject(gs), check); !apierrors.IsNotFound(err) {
		t.Fatalf("object %v still exists", gs)
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

func assertGitOpsSetCondition(t *testing.T, gs *templatesv1.GitOpsSet, condType, msg string) {
	cond := apimeta.FindStatusCondition(gs.Status.Conditions, condType)
	if cond == nil {
		t.Fatalf("failed to find matching status condition for type %s in %#v", condType, gs.Status.Conditions)
	}
	if cond.Message != msg {
		t.Fatalf("got %s, want %s", cond.Message, msg)
	}
}

func assertInventoryHasItems(t *testing.T, gs *templatesv1.GitOpsSet, objs ...runtime.Object) {
	t.Helper()
	if l := len(gs.Status.Inventory.Entries); l != len(objs) {
		t.Errorf("expected %d items, got %v", len(objs), l)
	}
	entries := []templatesv1.ResourceRef{}
	for _, obj := range objs {
		ref, err := resourceRefFromObject(obj)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, ref)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})
	want := &templatesv1.ResourceInventory{Entries: entries}
	if diff := cmp.Diff(want, gs.Status.Inventory); diff != "" {
		t.Errorf("failed to get inventory:\n%s", diff)
	}
}

func cleanupResource(t *testing.T, cl client.Client, obj client.Object) {
	if err := cl.Delete(context.TODO(), obj); err != nil {
		t.Fatal(err)
	}
}

func makeTestGitOpsSet(t *testing.T, opts ...func(*templatesv1.GitOpsSet)) *templatesv1.GitOpsSet {
	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-set",
			Namespace: "default",
		},
		Spec: templatesv1.GitOpsSetSpec{
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
					RawExtension: runtime.RawExtension{
						Raw: mustMarshalJSON(t, makeTestKustomization(nsn("default", "{{.cluster}}-demo"))),
						// , func(gs *kustomizev1.Kustomization) {
						// 				gs.Spec = kustomizev1.KustomizationSpec{
						// 					Interval: metav1.Duration{Duration: 5 * time.Minute},
						// 					Path:     "./clusters/{{.cluster}}/",
						// 					Prune:    true,
						// 					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						// 						Kind: "GitRepository",
						// 						Name: "demo-repo",
						// 					},
						// 					KubeConfig: &meta.KubeConfigReference{
						// 						SecretRef: meta.SecretKeyReference{
						// 							Name: "{{.cluster}}",
						// 						},
						// 					},
						// 				}
						// })),
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

func objectMetaIgnore() []cmp.Option {
	return []cmp.Option{
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "UID", "ResourceVersion", "Generation", "CreationTimestamp", "ManagedFields"),
	}
}

func mustMarshalJSON(t *testing.T, r runtime.Object) []byte {
	b, err := json.Marshal(r)
	test.AssertNoError(t, err)

	return b
}

func makeTestKustomization(name types.NamespacedName, opts ...func(*kustomizev1.Kustomization)) *kustomizev1.Kustomization {
	k := &kustomizev1.Kustomization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Kustomization",
			APIVersion: "kustomize.toolkit.fluxcd.io/v1beta2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
		Spec: kustomizev1.KustomizationSpec{
			Interval: metav1.Duration{Duration: 5 * time.Minute},
			Path:     "./examples/kustomize/environments/dev",
			Prune:    true,
			SourceRef: kustomizev1.CrossNamespaceSourceReference{
				Kind: "GitRepository",
				Name: "demo-repo",
			},
		},
	}

	for _, opt := range opts {
		opt(k)
	}

	return k
}

func nsn(namespace, name string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}
