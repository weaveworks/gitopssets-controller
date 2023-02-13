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
	"github.com/google/go-cmp/cmp/cmpopts"
	rbacv1 "k8s.io/api/rbac/v1"
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

	t.Run("reconciling creation when suspended", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Suspend = true
		})
		test.AssertNoError(t, k8sClient.Create(ctx, gs))

		defer cleanupResource(t, k8sClient, gs)
		defer deleteAllKustomizations(t, k8sClient)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		updated := &templatesv1.GitOpsSet{}
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated))

		assertInventoryHasNoItems(t, updated)
		assertNoKustomizationsExistInNamespace(t, k8sClient, "default")
	})

	t.Run("error conditions", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, makeTestKustomization(nsn("unused", "unused"), func(ks *kustomizev1.Kustomization) {
							ks.Name = "{{ .element.cluster }}-demo"
							ks.Annotations = map[string]string{
								"testing.cluster": "{{ .element.cluster }}",
								"testing":         "newVersion",
							}
							ks.Spec.Path = "./templated/clusters/{{ .element.cluster }}/"
							ks.Spec.KubeConfig = &meta.KubeConfigReference{SecretRef: meta.SecretKeyReference{Name: "{{ .element.cluster }}"}}
							ks.Spec.Force = true
						})),
					},
				},
			}
		})
		test.AssertNoError(t, k8sClient.Create(ctx, gs))

		devKS := makeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
			k.ObjectMeta.Annotations = map[string]string{
				"testing": "existingResource",
			}
		})
		test.AssertNoError(t, k8sClient.Create(ctx, test.ToUnstructured(t, devKS)))
		defer deleteAllKustomizations(t, k8sClient)
		defer cleanupResource(t, k8sClient, gs)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertErrorMatch(t, "failed to create Resource.*already exists", err)

		updated := &templatesv1.GitOpsSet{}
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated))
		assertGitOpsSetCondition(t, updated, meta.ReadyCondition, "failed to create Resource: kustomizations.kustomize.toolkit.fluxcd.io \"engineering-dev-demo\" already exists")
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

	t.Run("reconciling update of resources", func(t *testing.T) {
		ctx := context.TODO()
		devKS := makeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
			k.ObjectMeta.Annotations = map[string]string{
				"testing": "existingResource",
			}
		})
		test.AssertNoError(t, k8sClient.Create(ctx, test.ToUnstructured(t, devKS)))
		defer deleteAllKustomizations(t, k8sClient)

		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, makeTestKustomization(nsn("unused", "unused"), func(ks *kustomizev1.Kustomization) {
							ks.Name = "{{ .element.cluster }}-demo"
							ks.Annotations = map[string]string{
								"testing.cluster": "{{ .element.cluster }}",
								"testing":         "newVersion",
							}
							ks.Spec.Path = "./templated/clusters/{{ .element.cluster }}/"
							ks.Spec.KubeConfig = &meta.KubeConfigReference{SecretRef: meta.SecretKeyReference{Name: "{{ .element.cluster }}"}}
							ks.Spec.Force = true
						})),
					},
				},
			}

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
		defer cleanupResource(t, k8sClient, gs)

		ref, err := resourceRefFromObject(devKS)
		test.AssertNoError(t, err)
		gs.Status.Inventory = &templatesv1.ResourceInventory{
			Entries: []templatesv1.ResourceRef{ref},
		}
		if err := k8sClient.Status().Update(ctx, gs); err != nil {
			t.Fatal(err)
		}

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		if err != nil {
			t.Fatal(err)
		}

		updated := &templatesv1.GitOpsSet{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated); err != nil {
			t.Fatal(err)
		}
		wantUpdated := makeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
			k.ObjectMeta.Annotations = map[string]string{
				"testing.cluster": "engineering-dev",
				"testing":         "newVersion",
			}
			k.ObjectMeta.Labels = map[string]string{
				"templates.weave.works/name":      "demo-set",
				"templates.weave.works/namespace": "default",
			}
			k.Spec.Path = "./templated/clusters/engineering-dev/"
			k.Spec.KubeConfig = &meta.KubeConfigReference{SecretRef: meta.SecretKeyReference{Name: "engineering-dev"}}
			k.Spec.Force = true
		})

		want := []runtime.Object{
			wantUpdated,
		}
		assertInventoryHasItems(t, updated, want...)

		kustomization := &unstructured.Unstructured{}
		kustomization.SetGroupVersionKind(kustomizationGVK)
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(wantUpdated), kustomization); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(test.ToUnstructured(t, wantUpdated), kustomization, objectMetaIgnore()...); diff != "" {
			t.Fatalf("failed to update Kustomization:\n%s", diff)
		}
	})

	t.Run("reconciling with no generated resources", func(t *testing.T) {
		ctx := context.TODO()
		devKS := makeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
			k.ObjectMeta.Annotations = map[string]string{
				"testing": "existingResource",
			}
		})
		test.AssertNoError(t, k8sClient.Create(ctx, test.ToUnstructured(t, devKS)))
		defer deleteAllKustomizations(t, k8sClient)

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
		defer cleanupResource(t, k8sClient, gs)

		ref, err := resourceRefFromObject(devKS)
		test.AssertNoError(t, err)

		gs.Status.Inventory = &templatesv1.ResourceInventory{
			Entries: []templatesv1.ResourceRef{ref},
		}
		if err := k8sClient.Status().Update(ctx, gs); err != nil {
			t.Fatal(err)
		}

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

	t.Run("reconciling update of deleted resource", func(t *testing.T) {
		ctx := context.TODO()
		devKS := makeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
			k.ObjectMeta.Annotations = map[string]string{
				"testing": "existingResource",
			}
		})
		test.AssertNoError(t, k8sClient.Create(ctx, test.ToUnstructured(t, devKS)))
		defer deleteAllKustomizations(t, k8sClient)

		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, makeTestKustomization(nsn("unused", "unused"), func(ks *kustomizev1.Kustomization) {
							ks.Name = "{{ .element.cluster }}-demo"
							ks.Annotations = map[string]string{
								"testing.cluster": "{{ .element.cluster }}",
								"testing":         "newVersion",
							}
							ks.Spec.Path = "./templated/clusters/{{ .element.cluster }}/"
							ks.Spec.KubeConfig = &meta.KubeConfigReference{SecretRef: meta.SecretKeyReference{Name: "{{ .element.cluster }}"}}
							ks.Spec.Force = true
						})),
					},
				},
			}

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
		defer cleanupResource(t, k8sClient, gs)

		ref, err := resourceRefFromObject(devKS)
		test.AssertNoError(t, err)
		gs.Status.Inventory = &templatesv1.ResourceInventory{
			Entries: []templatesv1.ResourceRef{ref},
		}
		if err := k8sClient.Status().Update(ctx, gs); err != nil {
			t.Fatal(err)
		}
		// Delete the Kustomizations before reconciling
		deleteAllKustomizations(t, k8sClient)

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		if err != nil {
			t.Fatal(err)
		}

		updated := &templatesv1.GitOpsSet{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated); err != nil {
			t.Fatal(err)
		}

		want := []runtime.Object{
			makeTestKustomization(nsn("default", "engineering-dev-demo")),
		}
		assertInventoryHasItems(t, updated, want...)
		assertGitOpsSetCondition(t, updated, meta.ReadyCondition, "1 resources created")
		assertKustomizationsExist(t, k8sClient, "default", "engineering-dev-demo")
	})

	t.Run("service account impersonation", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.ServiceAccountName = "test-sa"
		})
		test.AssertNoError(t, k8sClient.Create(ctx, gs))

		defer cleanupResource(t, k8sClient, gs)
		defer deleteAllKustomizations(t, k8sClient)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertErrorMatch(t, `create Resource: kustomizations.* is forbidden: User "system:serviceaccount:default:test-sa"`, err)

		// Now create a service account granting the right permissions to create
		// Kustomizations in the right namespace.
		createRBACForServiceAccount(t, k8sClient, "test-sa", "default")

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
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

	t.Run("default service account impersonation", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t)
		test.AssertNoError(t, k8sClient.Create(ctx, gs))

		defer cleanupResource(t, k8sClient, gs)
		defer deleteAllKustomizations(t, k8sClient)

		reconciler.DefaultServiceAccount = "default-test-sa"
		defer func() {
			reconciler.DefaultServiceAccount = ""
		}()

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertErrorMatch(t, `create Resource: kustomizations.* is forbidden: User "system:serviceaccount:default:default-test-sa"`, err)

		// Now create a service account granting the right permissions to create
		// Kustomizations in the right namespace.
		createRBACForServiceAccount(t, k8sClient, "default-test-sa", "default")

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
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

	t.Run("reconciling update of resources with service account", func(t *testing.T) {
		ctx := context.TODO()
		devKS := makeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
			k.ObjectMeta.Annotations = map[string]string{
				"testing": "existingResource",
			}
		})
		test.AssertNoError(t, k8sClient.Create(ctx, test.ToUnstructured(t, devKS)))
		defer deleteAllKustomizations(t, k8sClient)

		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.ServiceAccountName = "test-sa"
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, makeTestKustomization(nsn("unused", "unused"), func(ks *kustomizev1.Kustomization) {
							ks.Name = "{{ .element.cluster }}-demo"
							ks.Spec.Path = "./templated/clusters/{{ .element.cluster }}/"
							ks.Spec.Force = true
						})),
					},
				},
			}
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
		defer cleanupResource(t, k8sClient, gs)
		// Note that this sets up the permissions to read only
		// This asserts that we can read the resource, and update the resource.
		createRBACForServiceAccount(t, k8sClient, "test-sa", "default", rbacv1.PolicyRule{
			APIGroups: []string{"kustomize.toolkit.fluxcd.io"},
			Resources: []string{"kustomizations"},
			Verbs:     []string{"get", "list", "watch"},
		})

		ref, err := resourceRefFromObject(devKS)
		test.AssertNoError(t, err)
		gs.Status.Inventory = &templatesv1.ResourceInventory{
			Entries: []templatesv1.ResourceRef{ref},
		}
		if err := k8sClient.Status().Update(ctx, gs); err != nil {
			t.Fatal(err)
		}

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertErrorMatch(t, `update Resource: kustomizations.* is forbidden: User "system:serviceaccount:default:test-sa"`, err)

		var role rbacv1.Role
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-role", Namespace: "default"}, &role); err != nil {
			t.Fatal(err)
		}
		role.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{"kustomize.toolkit.fluxcd.io"},
				Resources: []string{"kustomizations"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
		}
		if err := k8sClient.Update(ctx, &role); err != nil {
			t.Fatal(err)
		}

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		updated := &templatesv1.GitOpsSet{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated); err != nil {
			t.Fatal(err)
		}
		wantUpdated := makeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
			k.ObjectMeta.Labels = map[string]string{
				"templates.weave.works/name":      "demo-set",
				"templates.weave.works/namespace": "default",
			}
			k.Spec.Path = "./templated/clusters/engineering-dev/"
			k.Spec.Force = true
		})

		want := []runtime.Object{
			wantUpdated,
		}
		assertInventoryHasItems(t, updated, want...)

		kustomization := &unstructured.Unstructured{}
		kustomization.SetGroupVersionKind(kustomizationGVK)
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(wantUpdated), kustomization); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(test.ToUnstructured(t, wantUpdated), kustomization, objectMetaIgnore()...); diff != "" {
			t.Fatalf("failed to update Kustomization:\n%s", diff)
		}
	})

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

func assertInventoryHasNoItems(t *testing.T, gs *templatesv1.GitOpsSet) {
	t.Helper()
	if gs.Status.Inventory == nil {
		return
	}

	if l := len(gs.Status.Inventory.Entries); l != 0 {
		t.Errorf("expected inventory to have 0 items, got %v", l)
	}
}

func cleanupResource(t *testing.T, cl client.Client, obj client.Object) {
	t.Helper()
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
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, makeTestKustomization(nsn("default", "{{ .element.cluster }}-demo"), func(k *kustomizev1.Kustomization) {
							k.Spec = kustomizev1.KustomizationSpec{
								Interval: metav1.Duration{Duration: 5 * time.Minute},
								Path:     "./clusters/{{ .element.cluster }}/",
								Prune:    true,
								SourceRef: kustomizev1.CrossNamespaceSourceReference{
									Kind: "GitRepository",
									Name: "demo-repo",
								},
								KubeConfig: &meta.KubeConfigReference{
									SecretRef: meta.SecretKeyReference{
										Name: "{{ .element.cluster }}",
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

func objectMetaIgnore() []cmp.Option {
	metaFields := []string{"uid", "resourceVersion", "generation", "creationTimestamp", "managedFields", "status"}
	return []cmp.Option{
		cmpopts.IgnoreMapEntries(func(k, v any) bool {
			for _, key := range metaFields {
				if key == k {
					return true
				}
			}

			return false
		}),
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

func createRBACForServiceAccount(t *testing.T, cl client.Client, serviceAccountName, namespace string, rules ...rbacv1.PolicyRule) {
	if len(rules) == 0 {
		rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{"kustomize.toolkit.fluxcd.io"},
				Resources: []string{"kustomizations"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
		}

	}
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "test-role", Namespace: namespace},
		Rules:      rules,
	}
	if err := cl.Create(context.TODO(), role); err != nil {
		t.Fatalf("failed to write role: %s", err)
	}
	t.Cleanup(func() {
		cleanupResource(t, cl, role)
	})
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-role-binding", Namespace: namespace},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccountName,
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     role.Name,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	if err := cl.Create(context.TODO(), binding); err != nil {
		t.Fatalf("failed to write role-binding: %s", err)
	}
	t.Cleanup(func() {
		cleanupResource(t, cl, binding)
	})
}
