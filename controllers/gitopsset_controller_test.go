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
	fluxMeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators/gitrepository"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators/list"
	"github.com/gitops-tools/gitopssets-controller/test"
	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
)

var kustomizationGVK = schema.GroupVersionKind{
	Group:   "kustomize.toolkit.fluxcd.io",
	Kind:    "Kustomization",
	Version: "v1beta2",
}

var configMapGVK = schema.GroupVersionKind{
	Group:   "",
	Kind:    "ConfigMap",
	Version: "v1",
}

func TestReconciliation(t *testing.T) {
	testEnv := &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join("..", "config", "crd", "bases"),
			"testdata/crds",
			"../tests/e2e/testdata/crds",
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
	test.AssertNoError(t, sourcev1beta2.AddToScheme(scheme))

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	test.AssertNoError(t, err)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme})
	test.AssertNoError(t, err)
	eventRecorder := &test.FakeEventRecorder{}

	reconciler := &GitOpsSetReconciler{
		Client:                k8sClient,
		Scheme:                scheme,
		DefaultServiceAccount: "",
		Generators: map[string]generators.GeneratorFactory{
			"List":          list.GeneratorFactory,
			"GitRepository": gitrepository.GeneratorFactory(nil),
		},
		Config:        cfg,
		EventRecorder: eventRecorder,
	}

	test.AssertNoError(t, reconciler.SetupWithManager(mgr))
	test.AssertNoError(t, k8sClient.Create(context.TODO(), test.NewNamespace("test-ns")))

	t.Run("reconciling creation of new resources", func(t *testing.T) {
		ctx := context.TODO()
		gs := createAndReconcileToFinalizedState(t, k8sClient, reconciler, makeTestGitOpsSet(t))
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		updated := &templatesv1.GitOpsSet{}
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated))

		want := []runtime.Object{
			test.MakeTestKustomization(nsn("default", "engineering-dev-demo")),
			test.MakeTestKustomization(nsn("default", "engineering-prod-demo")),
			test.MakeTestKustomization(nsn("default", "engineering-preprod-demo")),
		}
		test.AssertInventoryHasItems(t, updated, want...)
		assertGitOpsSetCondition(t, updated, meta.ReadyCondition, "3 resources created")
		assertKustomizationsExist(t, k8sClient, "default", "engineering-dev-demo", "engineering-prod-demo", "engineering-preprod-demo")
	})

	t.Run("reconciling creation of resources in different namespaces", func(t *testing.T) {
		ctx := context.TODO()

		// https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
		test.AssertNoError(t, k8sClient.Create(ctx, test.NewNamespace("engineering-dev-ns")))
		test.AssertNoError(t, k8sClient.Create(ctx, test.NewNamespace("engineering-preprod-ns")))
		test.AssertNoError(t, k8sClient.Create(ctx, test.NewNamespace("engineering-prod-ns")))
		gs := createAndReconcileToFinalizedState(t, k8sClient, reconciler, makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("{{ .Element.cluster }}-ns", "demo-kustomization"))),
					},
				},
			}
		}))
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))

		want := []runtime.Object{
			test.MakeTestKustomization(nsn("engineering-dev-ns", "demo-kustomization")),
			test.MakeTestKustomization(nsn("engineering-prod-ns", "demo-kustomization")),
			test.MakeTestKustomization(nsn("engineering-preprod-ns", "demo-kustomization")),
		}
		test.AssertInventoryHasItems(t, gs, want...)
		assertGitOpsSetCondition(t, gs, meta.ReadyCondition, "3 resources created")
	})

	t.Run("reconciling cleanup when deleted", func(t *testing.T) {
		ctx := context.TODO()
		gs := createAndReconcileToFinalizedState(t, k8sClient, reconciler, makeTestGitOpsSet(t))
		// manually deletes below

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))

		want := []runtime.Object{
			test.MakeTestKustomization(nsn("default", "engineering-dev-demo")),
			test.MakeTestKustomization(nsn("default", "engineering-prod-demo")),
			test.MakeTestKustomization(nsn("default", "engineering-preprod-demo")),
		}
		test.AssertInventoryHasItems(t, gs, want...)
		assertGitOpsSetCondition(t, gs, meta.ReadyCondition, "3 resources created")
		assertKustomizationsExist(t, k8sClient, "default", "engineering-dev-demo", "engineering-prod-demo", "engineering-preprod-demo")

		deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)
		assertNoKustomizationsExistInNamespace(t, k8sClient, "default")
	})

	t.Run("error conditions", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("", "unused"), func(ks *kustomizev1.Kustomization) {
							ks.Name = "{{ .Element.cluster }}-demo"
							ks.Annotations = map[string]string{
								"testing.cluster": "{{ .Element.cluster }}",
								"testing":         "newVersion",
							}
							ks.Spec.Path = "./templated/clusters/{{ .Element.cluster }}/"
							ks.Spec.KubeConfig = &meta.KubeConfigReference{SecretRef: meta.SecretKeyReference{Name: "{{ .Element.cluster }}"}}
							ks.Spec.Force = true
						})),
					},
				},
			}
		})
		gs = createAndReconcileToFinalizedState(t, k8sClient, reconciler, gs)
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		devKS := test.MakeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
			k.ObjectMeta.Annotations = map[string]string{
				"testing": "existingResource",
			}
		})
		test.AssertNoError(t, k8sClient.Create(ctx, test.ToUnstructured(t, devKS)))
		defer deleteObject(t, k8sClient, test.ToUnstructured(t, devKS))

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertErrorMatch(t, "failed to create Resource.*already exists", err)

		updated := &templatesv1.GitOpsSet{}
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated))
		assertGitOpsSetCondition(t, updated, meta.ReadyCondition, "failed to create Resource: kustomizations.kustomize.toolkit.fluxcd.io \"engineering-dev-demo\" already exists")
	})

	t.Run("reconciling removal of resources", func(t *testing.T) {
		ctx := context.TODO()
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
		gs = createAndReconcileToFinalizedState(t, k8sClient, reconciler, gs)
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)
		// Initial creation of resources
		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))
		want := []runtime.Object{
			test.MakeTestKustomization(nsn("default", "engineering-prod-demo")),
			test.MakeTestKustomization(nsn("default", "engineering-preprod-demo")),
		}
		test.AssertInventoryHasItems(t, gs, want...)

		gs.Spec.Generators = []templatesv1.GitOpsSetGenerator{
			{
				List: &templatesv1.ListGenerator{
					Elements: []apiextensionsv1.JSON{
						{Raw: []byte(`{"cluster": "engineering-prod"}`)},
						{Raw: []byte(`{"cluster": "engineering-staging"}`)},
					},
				},
			},
		}
		test.AssertNoError(t, k8sClient.Update(ctx, gs))

		// Updated set of resources.
		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))

		want = []runtime.Object{
			test.MakeTestKustomization(nsn("default", "engineering-prod-demo")),
			test.MakeTestKustomization(nsn("default", "engineering-staging-demo")),
		}
		test.AssertInventoryHasItems(t, gs, want...)
		assertResourceDoesNotExist(t, k8sClient, test.MakeTestKustomization(nsn("default", "engineering-preprod-demo")))
	})

	t.Run("reconciling update of resources", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("", "unused"), func(ks *kustomizev1.Kustomization) {
							ks.Name = "{{ .Element.cluster }}-demo"
							ks.Annotations = map[string]string{
								"example.com/cluster": "{{ .Element.cluster }}",
								"testing":             "oldVersion",
							}
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

		gs = createAndReconcileToFinalizedState(t, k8sClient, reconciler, gs)
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		// Initial creation of resources
		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		// Updating the GitOpsSet and resources
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))
		gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
			{
				Content: runtime.RawExtension{
					Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("", "unused"), func(ks *kustomizev1.Kustomization) {
						ks.Name = "{{ .Element.cluster }}-demo"
						ks.Annotations = map[string]string{
							"testing.cluster": "{{ .Element.cluster }}",
							"testing":         "newVersion",
						}
						ks.Spec.Path = "./templated/clusters/{{ .Element.cluster }}/"
						ks.Spec.KubeConfig = &meta.KubeConfigReference{SecretRef: meta.SecretKeyReference{Name: "{{ .Element.cluster }}"}}
						ks.Spec.Force = true
					})),
				},
			},
		}
		test.AssertNoError(t, k8sClient.Update(ctx, gs))

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))
		wantUpdated := test.MakeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
			k.ObjectMeta.Annotations = map[string]string{
				"testing.cluster": "engineering-dev",
				"testing":         "newVersion",
			}
			k.ObjectMeta.Labels = map[string]string{
				"sets.gitops.pro/name":      "demo-set",
				"sets.gitops.pro/namespace": "default",
			}
			k.Spec.Path = "./templated/clusters/engineering-dev/"
			k.Spec.KubeConfig = &meta.KubeConfigReference{SecretRef: meta.SecretKeyReference{Name: "engineering-dev"}}
			k.Spec.Force = true
		})

		want := []runtime.Object{
			wantUpdated,
		}
		test.AssertInventoryHasItems(t, gs, want...)

		kustomization := &unstructured.Unstructured{}
		kustomization.SetGroupVersionKind(kustomizationGVK)
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(wantUpdated), kustomization))

		if diff := cmp.Diff(test.ToUnstructured(t, wantUpdated), kustomization, objectMetaIgnore()); diff != "" {
			t.Fatalf("failed to update Kustomization:\n%s", diff)
		}
	})

	t.Run("reconciling update of configmaps", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.NewConfigMap(func(c *corev1.ConfigMap) {
							c.Data = map[string]string{
								"testing": "{{ .Element.configValue }}",
							}
						})),
					},
				},
			}

			gs.Spec.Generators = []templatesv1.GitOpsSetGenerator{
				{
					List: &templatesv1.ListGenerator{
						Elements: []apiextensionsv1.JSON{
							{Raw: []byte(`{"cluster": "engineering-dev","configValue":"test-value1"}`)},
						},
					},
				},
			}
		})
		gs = createAndReconcileToFinalizedState(t, k8sClient, reconciler, gs)
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		// Initial creation of ConfigMaps
		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		updated := &templatesv1.GitOpsSet{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated); err != nil {
			t.Fatal(err)
		}
		wantCM := test.NewConfigMap(func(c *corev1.ConfigMap) {
			c.ObjectMeta.Labels = map[string]string{
				"sets.gitops.pro/name":      "demo-set",
				"sets.gitops.pro/namespace": "default",
			}
			c.Data = map[string]string{
				"testing": "test-value1",
			}
		})
		want := []runtime.Object{
			wantCM,
		}
		test.AssertInventoryHasItems(t, updated, want...)

		createdCM := &unstructured.Unstructured{}
		createdCM.SetGroupVersionKind(configMapGVK)
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(wantCM), createdCM))

		if diff := cmp.Diff(test.ToUnstructured(t, wantCM), createdCM, objectMetaIgnore()); diff != "" {
			t.Fatalf("failed to create ConfigMap:\n%s", diff)
		}

		// Updating the ConfigMap generation
		updated.Spec.Generators = []templatesv1.GitOpsSetGenerator{
			{
				List: &templatesv1.ListGenerator{
					Elements: []apiextensionsv1.JSON{
						{Raw: []byte(`{"cluster": "engineering-dev","configValue":"test-value2"}`)},
					},
				},
			},
		}
		test.AssertNoError(t, k8sClient.Update(ctx, updated))

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		wantCM = test.NewConfigMap(func(c *corev1.ConfigMap) {
			c.ObjectMeta.Labels = map[string]string{
				"sets.gitops.pro/name":      "demo-set",
				"sets.gitops.pro/namespace": "default",
			}
			c.Data = map[string]string{
				"testing": "test-value2",
			}
		})

		updatedCM := &unstructured.Unstructured{}
		updatedCM.SetGroupVersionKind(configMapGVK)
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(wantCM), updatedCM))

		if diff := cmp.Diff(test.ToUnstructured(t, wantCM), updatedCM, objectMetaIgnore()); diff != "" {
			t.Fatalf("failed to update ConfigMap:\n%s", diff)
		}
	})

	t.Run("reconciling with no generated resources", func(t *testing.T) {
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
		gs = createAndReconcileToFinalizedState(t, k8sClient, reconciler, gs)
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))
		assertInventoryHasNoItems(t, gs)
	})

	t.Run("reconciling update of deleted resource", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("", "unused"), func(ks *kustomizev1.Kustomization) {
							ks.Name = "{{ .Element.cluster }}-demo"
							ks.Annotations = map[string]string{
								"testing.cluster": "{{ .Element.cluster }}",
								"testing":         "newVersion",
							}
							ks.Spec.Path = "./templated/clusters/{{ .Element.cluster }}/"
							ks.Spec.KubeConfig = &meta.KubeConfigReference{SecretRef: meta.SecretKeyReference{Name: "{{ .Element.cluster }}"}}
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
		gs = createAndReconcileToFinalizedState(t, k8sClient, reconciler, gs)
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		// Initial creation of resources
		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		want := []runtime.Object{
			test.MakeTestKustomization(nsn("default", "engineering-dev-demo")),
		}
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))
		test.AssertInventoryHasItems(t, gs, want...)

		// Delete the Kustomizations before reconciling
		deleteAllKustomizations(t, k8sClient)

		// Second reconciliation - handling deleted kustomizations.
		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		if err != nil {
			t.Fatal(err)
		}

		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))
		test.AssertInventoryHasItems(t, gs, want...)
		assertGitOpsSetCondition(t, gs, meta.ReadyCondition, "1 resources created")
		assertKustomizationsExist(t, k8sClient, "default", "engineering-dev-demo")
	})

	t.Run("service account impersonation", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.ServiceAccountName = "test-sa"
		})
		gs = createAndReconcileToFinalizedState(t, k8sClient, reconciler, gs)
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

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
			test.MakeTestKustomization(nsn("default", "engineering-dev-demo")),
			test.MakeTestKustomization(nsn("default", "engineering-prod-demo")),
			test.MakeTestKustomization(nsn("default", "engineering-preprod-demo")),
		}
		test.AssertInventoryHasItems(t, updated, want...)
		assertGitOpsSetCondition(t, updated, meta.ReadyCondition, "3 resources created")
		assertKustomizationsExist(t, k8sClient, "default", "engineering-dev-demo", "engineering-prod-demo", "engineering-preprod-demo")
	})

	t.Run("default service account impersonation", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t)
		gs = createAndReconcileToFinalizedState(t, k8sClient, reconciler, gs)
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

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
			test.MakeTestKustomization(nsn("default", "engineering-dev-demo")),
			test.MakeTestKustomization(nsn("default", "engineering-prod-demo")),
			test.MakeTestKustomization(nsn("default", "engineering-preprod-demo")),
		}
		test.AssertInventoryHasItems(t, updated, want...)
		assertGitOpsSetCondition(t, updated, meta.ReadyCondition, "3 resources created")
		assertKustomizationsExist(t, k8sClient, "default", "engineering-dev-demo", "engineering-prod-demo", "engineering-preprod-demo")
	})

	t.Run("reconciling update of resources with service account", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("default", "unused"), func(ks *kustomizev1.Kustomization) {
							ks.Name = "{{ .Element.cluster }}-demo"
							ks.Spec.Path = "./templated/clusters/{{ .Element.cluster }}/"
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
		gs = createAndReconcileToFinalizedState(t, k8sClient, reconciler, gs)
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))
		assertGitOpsSetCondition(t, gs, meta.ReadyCondition, "1 resources created")

		gs.Spec.ServiceAccountName = "test-sa"
		gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
			{
				Content: runtime.RawExtension{
					Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("", "unused"), func(ks *kustomizev1.Kustomization) {
						ks.Name = "{{ .Element.cluster }}-demo"
						ks.Spec.Path = "./templated/clusters/{{ .Element.cluster }}/"
						ks.Spec.Force = true
					})),
				},
			},
		}
		test.AssertNoError(t, k8sClient.Update(ctx, gs))

		// Note that this sets up the permissions to read only
		// This asserts that we can read the resource, and update the resource.
		createRBACForServiceAccount(t, k8sClient, "test-sa", "default", rbacv1.PolicyRule{
			APIGroups: []string{"kustomize.toolkit.fluxcd.io"},
			Resources: []string{"kustomizations"},
			Verbs:     []string{"get", "list", "watch"},
		})

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertErrorMatch(t, `update Resource: kustomizations.* is forbidden: User "system:serviceaccount:default:test-sa"`, err)

		// Switch the permissions to allow updating.
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
		test.AssertNoError(t, k8sClient.Update(ctx, &role))

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		updated := &templatesv1.GitOpsSet{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated); err != nil {
			t.Fatal(err)
		}
		wantUpdated := test.MakeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
			k.ObjectMeta.Labels = map[string]string{
				"sets.gitops.pro/name":      "demo-set",
				"sets.gitops.pro/namespace": "default",
			}
			k.Spec.Path = "./templated/clusters/engineering-dev/"
			k.Spec.Force = true
		})

		want := []runtime.Object{
			wantUpdated,
		}
		test.AssertInventoryHasItems(t, updated, want...)

		kustomization := &unstructured.Unstructured{}
		kustomization.SetGroupVersionKind(kustomizationGVK)
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(wantUpdated), kustomization); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(test.ToUnstructured(t, wantUpdated), kustomization, objectMetaIgnore()); diff != "" {
			t.Fatalf("failed to update Kustomization:\n%s", diff)
		}
	})

	t.Run("reconciling with annotation-triggered reconciliation", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.ObjectMeta.Annotations = map[string]string{
				fluxMeta.ReconcileRequestAnnotation: time.Now().Format(time.RFC3339Nano),
			}
		})
		gs = createAndReconcileToFinalizedState(t, k8sClient, reconciler, gs)
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		updated := &templatesv1.GitOpsSet{}
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated))

		if updated.Status.ReconcileRequestStatus.LastHandledReconcileAt == "" {
			t.Fatal("expected the Status to include the timestamp of the reconciliation")
		}
	})

	t.Run("reconciling creation when suspended", func(t *testing.T) {
		ctx := context.TODO()
		gs := createAndReconcileToFinalizedState(t, k8sClient, reconciler, makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Suspend = true
		}))
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))
		assertInventoryHasNoItems(t, gs)
		assertNoKustomizationsExistInNamespace(t, k8sClient, "default")
	})

	t.Run("reconciling when gitrepository has no artifact", func(t *testing.T) {
		ctx := context.TODO()
		emptyGR := test.NewGitRepository()
		test.AssertNoError(t, k8sClient.Create(ctx, test.ToUnstructured(t, emptyGR)))
		defer deleteObject(t, k8sClient, emptyGR)

		gs := createAndReconcileToFinalizedState(t, k8sClient, reconciler, makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Generators = []templatesv1.GitOpsSetGenerator{
				{
					GitRepository: &templatesv1.GitRepositoryGenerator{
						RepositoryRef: "test-repository",
						Files: []templatesv1.RepositoryGeneratorFileItem{
							{Path: "files/dev.yaml"},
							{Path: "files/production.yaml"},
							{Path: "files/staging.yaml"},
						},
					},
				},
			}
		}))
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertNoError(t, err)

		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), gs))
		assertInventoryHasNoItems(t, gs)
		assertNoKustomizationsExistInNamespace(t, k8sClient, "default")
		assertGitOpsSetCondition(t, gs, meta.ReadyCondition, "waiting for artifact")
	})

	t.Run("error conditions - existing resource", func(t *testing.T) {
		ctx := context.TODO()
		gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
			gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("", "unused"), func(ks *kustomizev1.Kustomization) {
							ks.Name = "{{ .Element.cluster }}-demo"
							ks.Annotations = map[string]string{
								"testing.cluster": "{{ .Element.cluster }}",
								"testing":         "newVersion",
							}
							ks.Spec.Path = "./templated/clusters/{{ .Element.cluster }}/"
							ks.Spec.KubeConfig = &meta.KubeConfigReference{SecretRef: meta.SecretKeyReference{Name: "{{ .Element.cluster }}"}}
							ks.Spec.Force = true
						})),
					},
				},
			}
		})
		gs = createAndReconcileToFinalizedState(t, k8sClient, reconciler, gs)
		defer deleteGitOpsSetAndFinalize(t, k8sClient, reconciler, gs)

		devKS := test.MakeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
			k.ObjectMeta.Annotations = map[string]string{
				"testing": "existingResource",
			}
		})
		test.AssertNoError(t, k8sClient.Create(ctx, test.ToUnstructured(t, devKS)))
		defer deleteObject(t, k8sClient, gs)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
		test.AssertErrorMatch(t, "failed to create Resource.*already exists", err)

		updated := &templatesv1.GitOpsSet{}
		test.AssertNoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(gs), updated))
		assertGitOpsSetCondition(t, updated, meta.ReadyCondition, "failed to create Resource: kustomizations.kustomize.toolkit.fluxcd.io \"engineering-dev-demo\" already exists")
	})
}

func TestGetClusterSelectors(t *testing.T) {
	testCases := []struct {
		name      string
		generator templatesv1.GitOpsSetGenerator
		want      []metav1.LabelSelector
	}{
		{
			name: "with cluster",
			generator: templatesv1.GitOpsSetGenerator{
				Cluster: &templatesv1.ClusterGenerator{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "myapp",
						},
					},
				},
			},
			want: []metav1.LabelSelector{
				{
					MatchLabels: map[string]string{
						"app": "myapp",
					},
				},
			},
		},
		{
			name: "with matrix",
			generator: templatesv1.GitOpsSetGenerator{
				Matrix: &templatesv1.MatrixGenerator{
					Generators: []templatesv1.GitOpsSetNestedGenerator{
						{
							Cluster: &templatesv1.ClusterGenerator{
								Selector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										"env": "prod",
									},
								},
							},
						},
						{
							Cluster: &templatesv1.ClusterGenerator{
								Selector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										"env": "staging",
									},
								},
							},
						},
					},
				},
			},
			want: []metav1.LabelSelector{
				{
					MatchLabels: map[string]string{
						"env": "prod",
					},
				},
				{
					MatchLabels: map[string]string{
						"env": "staging",
					},
				},
			},
		},
		{
			name:      "without cluster or matrix",
			generator: templatesv1.GitOpsSetGenerator{},
			want:      []metav1.LabelSelector{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := getClusterSelectors(tc.generator)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("failed to get selectors:\n%s", diff)
			}
		})
	}
}

func TestMatchCluster(t *testing.T) {
	gitopsCluster := &clustersv1.GitopsCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "myapp",
				"env": "prod",
			},
		},
	}

	clusterGen := &templatesv1.ClusterGenerator{
		Selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "myapp",
			},
		},
	}

	testCases := []struct {
		name      string
		cluster   *clustersv1.GitopsCluster
		gitopsSet *templatesv1.GitOpsSet
		want      bool
	}{
		{
			name:    "matching cluster",
			cluster: gitopsCluster,
			gitopsSet: &templatesv1.GitOpsSet{
				Spec: templatesv1.GitOpsSetSpec{
					Generators: []templatesv1.GitOpsSetGenerator{
						{
							Cluster: clusterGen,
						},
					},
				},
			},
			want: true,
		},
		{
			name:    "non-matching cluster",
			cluster: gitopsCluster,
			gitopsSet: &templatesv1.GitOpsSet{
				Spec: templatesv1.GitOpsSetSpec{
					Generators: []templatesv1.GitOpsSetGenerator{
						{
							Cluster: &templatesv1.ClusterGenerator{
								Selector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "myapp",
										"env": "staging",
									},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name:    "matching cluster in matrix generator",
			cluster: gitopsCluster,
			gitopsSet: &templatesv1.GitOpsSet{
				Spec: templatesv1.GitOpsSetSpec{
					Generators: []templatesv1.GitOpsSetGenerator{
						{
							Matrix: &templatesv1.MatrixGenerator{
								Generators: []templatesv1.GitOpsSetNestedGenerator{
									{
										Cluster: clusterGen,
									},
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name:    "list generator should not match",
			cluster: gitopsCluster,
			gitopsSet: &templatesv1.GitOpsSet{
				Spec: templatesv1.GitOpsSetSpec{
					Generators: []templatesv1.GitOpsSetGenerator{
						{
							List: &templatesv1.ListGenerator{},
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchCluster(tc.cluster, tc.gitopsSet)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("failed to match cluster:\n%s", diff)
			}
		})
	}
}

func TestSelectorMatchesCluster(t *testing.T) {
	testCases := []struct {
		name          string
		cluster       *clustersv1.GitopsCluster
		labelSelector metav1.LabelSelector
		want          bool
	}{
		{
			name: "matching selector",
			cluster: &clustersv1.GitopsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "myapp",
						"env": "prod",
					},
				},
			},
			labelSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "myapp",
				},
			},
			want: true,
		},
		{
			name: "non-matching selector",
			cluster: &clustersv1.GitopsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "myapp",
						"env": "prod",
					},
				},
			},
			labelSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "otherapp",
				},
			},
			want: false,
		},
		{
			name: "empty selector",
			cluster: &clustersv1.GitopsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "myapp",
						"env": "prod",
					},
				},
			},
			labelSelector: metav1.LabelSelector{},
			want:          false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectorMatchesCluster(tc.labelSelector, tc.cluster)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("selectorMatchesCluster(%v, %v) mismatch (-want +got):\n%s", tc.labelSelector, tc.cluster, diff)
			}
		})
	}
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

func assertInventoryHasNoItems(t *testing.T, gs *templatesv1.GitOpsSet) {
	t.Helper()
	if gs.Status.Inventory == nil {
		return
	}

	if l := len(gs.Status.Inventory.Entries); l != 0 {
		t.Errorf("expected inventory to have 0 items, got %v", l)
	}
}

func deleteObject(t *testing.T, cl client.Client, obj client.Object) {
	t.Helper()
	if err := cl.Delete(context.TODO(), obj); err != nil {
		t.Fatal(err)
	}
}

// Create the provided GitOpsSet and ensure that it has been setup for
// finalization.
func createAndReconcileToFinalizedState(t *testing.T, k8sClient client.Client, r *GitOpsSetReconciler, gs *templatesv1.GitOpsSet) *templatesv1.GitOpsSet {
	test.AssertNoError(t, k8sClient.Create(context.TODO(), gs))
	reconcileAndAssertFinalizerExists(t, k8sClient, r, gs)

	return gs
}

// this runs a single reconciliation and asserts that the set finalizer is
// applied
// This is needed because the reconciler returns after applying the finalizer to
// avoid race conditions.
func reconcileAndAssertFinalizerExists(t *testing.T, cl client.Client, reconciler *GitOpsSetReconciler, gs *templatesv1.GitOpsSet) {
	ctx := context.TODO()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
	test.AssertNoError(t, err)

	test.AssertNoError(t, cl.Get(ctx, client.ObjectKeyFromObject(gs), gs))
	if !controllerutil.ContainsFinalizer(gs, templatesv1.GitOpsSetFinalizer) {
		t.Fatal("GitOpsSet is missing the finalizer")
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

func objectMetaIgnore() cmp.Option {
	metaFields := []string{"uid", "resourceVersion", "generation", "creationTimestamp", "managedFields", "status", "ownerReferences"}
	return cmpopts.IgnoreMapEntries(func(k, v any) bool {
		for _, key := range metaFields {
			if key == k {
				return true
			}
		}

		return false
	})
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

func createRBACForServiceAccount(t *testing.T, cl client.Client, serviceAccountName, namespace string, rules ...rbacv1.PolicyRule) {
	t.Helper()
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
		deleteObject(t, cl, role)
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
		deleteObject(t, cl, binding)
	})
}
func deleteGitOpsSetAndFinalize(t *testing.T, cl client.Client, reconciler *GitOpsSetReconciler, gs *templatesv1.GitOpsSet) {
	t.Helper()
	ctx := context.TODO()
	if gs.Spec.Suspend {
		gs.Spec.Suspend = false
		test.AssertNoError(t, cl.Update(ctx, gs))
	}

	test.AssertNoError(t, cl.Delete(ctx, gs))

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)})
	test.AssertNoError(t, err)

	if !apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(gs), gs)) {
		t.Fatalf("failed to finalize: %s", err)
	}
	test.AssertNoError(t, client.IgnoreNotFound(cl.Get(ctx, client.ObjectKeyFromObject(gs), gs)))
}
