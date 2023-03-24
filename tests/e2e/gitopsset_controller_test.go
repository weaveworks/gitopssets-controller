package tests

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"testing"
	"time"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta2"
	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/gomega"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/test"
)

var kustomizationGVK = schema.GroupVersionKind{
	Group:   "kustomize.toolkit.fluxcd.io",
	Kind:    "Kustomization",
	Version: "v1beta2",
}

func TestReconcilingCreationOfNewResources(t *testing.T) {
	ctx := context.TODO()
	gs := makeTestGitOpsSet(t)
	test.AssertNoError(t, testEnv.Create(ctx, gs))

	waitForGitOpsSetCondition(t, testEnv, gs, "3 resources created")
	updated := &templatesv1.GitOpsSet{}
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), updated))

	if !controllerutil.ContainsFinalizer(updated, templatesv1.GitOpsSetFinalizer) {
		t.Fatal("GitOpsSet is missing the finalizer")
	}

	want := []runtime.Object{
		test.MakeTestKustomization(nsn("default", "engineering-dev-demo")),
		test.MakeTestKustomization(nsn("default", "engineering-prod-demo")),
		test.MakeTestKustomization(nsn("default", "engineering-preprod-demo")),
	}
	test.AssertInventoryHasItems(t, updated, want...)
	assertKustomizationsExist(t, testEnv, "default", "engineering-dev-demo", "engineering-prod-demo", "engineering-preprod-demo")

	deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)
	assertNoKustomizationsExistInNamespace(t, testEnv, "default")
}

func TestErrorReconciling(t *testing.T) {
	ctx := context.TODO()
	devKS := test.MakeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
		k.ObjectMeta.Annotations = map[string]string{
			"testing": "existingResource",
		}
	})
	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, devKS)))
	defer deleteObject(t, testEnv, test.ToUnstructured(t, devKS))

	gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
		gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
			{
				Content: runtime.RawExtension{
					Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("unused", "unused"), func(ks *kustomizev1.Kustomization) {
						ks.Name = "{{ .Element.cluster }}-demo"
						ks.Namespace = "default"
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
	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetCondition(t, testEnv, gs, "failed to create Resource: kustomizations.kustomize.toolkit.fluxcd.io \"engineering-dev-demo\" already exists")
}

func TestReconcilingRemovalOfResources(t *testing.T) {
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

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetCondition(t, testEnv, gs, "2 resources created")

	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), gs))
	devKS := test.MakeTestKustomization(nsn("default", "engineering-preprod-demo"))
	want := []runtime.Object{
		test.MakeTestKustomization(nsn("default", "engineering-prod-demo")),
		devKS,
	}
	test.AssertInventoryHasItems(t, gs, want...)

	gs.Spec.Generators = []templatesv1.GitOpsSetGenerator{
		{
			List: &templatesv1.ListGenerator{
				Elements: []apiextensionsv1.JSON{
					{Raw: []byte(`{"cluster": "engineering-prod"}`)},
				},
			},
		},
	}
	test.AssertNoError(t, testEnv.Update(ctx, gs))

	waitForGitOpsSetCondition(t, testEnv, gs, "1 resources created")
	updated := &templatesv1.GitOpsSet{}
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), updated))
	want = []runtime.Object{
		test.MakeTestKustomization(nsn("default", "engineering-prod-demo")),
	}
	test.AssertInventoryHasItems(t, updated, want...)
	assertKustomizationDoesNotExist(t, testEnv, devKS)
}

func TestReconcilingUpdateOfResources(t *testing.T) {
	ctx := context.TODO()
	gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
		gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
			{
				Content: runtime.RawExtension{
					Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("default", "unused"), func(ks *kustomizev1.Kustomization) {
						ks.Name = "{{ .Element.cluster }}-demo"
						ks.Annotations = map[string]string{
							"testing": "newVersion",
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
	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)
	waitForGitOpsSetCondition(t, testEnv, gs, "1 resources created")

	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), gs))
	gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
		{
			Content: runtime.RawExtension{
				Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("default", "unused"), func(ks *kustomizev1.Kustomization) {
					ks.Name = "{{ .Element.cluster }}-demo"
					ks.Annotations = map[string]string{
						"testing.cluster": "{{ .Element.cluster }}",
					}
					ks.Spec.Path = "./templated/clusters/{{ .Element.cluster }}/"
					ks.Spec.Force = true
				})),
			},
		},
	}
	test.AssertNoError(t, testEnv.Update(ctx, gs))

	wantUpdatedKustomization := test.MakeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
		k.ObjectMeta.Annotations = map[string]string{
			"testing.cluster": "engineering-dev",
		}
		k.ObjectMeta.Labels = map[string]string{
			"templates.weave.works/name":      "demo-set",
			"templates.weave.works/namespace": "default",
		}
		k.Spec.Path = "./templated/clusters/engineering-dev/"
		k.Spec.Force = true
	})

	kustomization := &unstructured.Unstructured{}
	// Wait for the update to the GitOpsSet to update the resources.
	g := gomega.NewWithT(t)
	g.Eventually(func() bool {
		kustomization.SetGroupVersionKind(kustomizationGVK)
		if err := testEnv.Get(ctx, client.ObjectKeyFromObject(wantUpdatedKustomization), kustomization); err != nil {
			t.Fatal(err)
		}

		return kustomization.GetAnnotations()["testing.cluster"] == "engineering-dev"
	}, timeout).Should(gomega.BeTrue())

	if diff := cmp.Diff(test.ToUnstructured(t, wantUpdatedKustomization), kustomization, objectMetaIgnore); diff != "" {
		t.Fatalf("failed to update Kustomization:\n%s", diff)
	}
}

func TestGitOpsSetUpdateOnGitRepoChange(t *testing.T) {
	ctx := context.TODO()

	// Create a GitRepository with a fake archive server.
	srv := test.StartFakeArchiveServer(t, "testdata/archive")
	gRepo := makeTestGitRepository(t, srv.URL+"/files.tar.gz")
	test.AssertNoError(t, testEnv.Create(ctx, gRepo))

	gRepo.Status = getGitRepoStatusUpdate(t, srv.URL+"/files.tar.gz", "f0a57ec1cdebda91cf00d89dfa298c6ac27791e7fdb0329990478061755eaca8")

	test.AssertNoError(t, testEnv.Status().Update(ctx, gRepo))

	// Create a GitOpsSet that uses the GitRepository.
	gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
		gs.Spec.Generators = []templatesv1.GitOpsSetGenerator{
			{
				Matrix: &templatesv1.MatrixGenerator{
					Generators: []templatesv1.GitOpsSetNestedGenerator{
						{
							GitRepository: &templatesv1.GitRepositoryGenerator{
								RepositoryRef: "my-git-repo",
								Files: []templatesv1.GitRepositoryGeneratorFileItem{
									{Path: "files/dev.yaml"},
								},
							},
						},
						{
							List: &templatesv1.ListGenerator{
								Elements: []apiextensionsv1.JSON{
									{Raw: []byte(`{"cluster": "eng-dev"}`)},
								},
							},
						},
					},
				},
			},
		}
		gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
			{
				Content: runtime.RawExtension{
					Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("default", "eng-{{ .Element.environment }}-demo"))),
				},
			},
		}
	})

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetInventory(t, testEnv, gs, test.MakeTestKustomization(nsn("default", "eng-dev-demo")))

	// Update the GitRepository to point to a new archive.
	gRepo.Status = getGitRepoStatusUpdate(t, srv.URL+"/files-develop.tar.gz", "14cc05e5d1206860b56630b41676dbfed05533011177e123c5dd48c72b959cdc")

	test.AssertNoError(t, testEnv.Status().Update(ctx, gRepo))

	waitForGitOpsSetInventory(t, testEnv, gs, test.MakeTestKustomization(nsn("default", "eng-development-demo")))
}

func TestServiceAccountImpersonation(t *testing.T) {
	ctx := context.TODO()
	gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
		gs.Spec.ServiceAccountName = "test-sa"
	})
	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetCondition(t, testEnv, gs, `failed to create Resource: kustomizations.* is forbidden: User \"system:serviceaccount:default:test-sa\".*`)

	// Now create a service account granting the right permissions to create
	// Kustomizations in the right namespace.
	createRBACForServiceAccount(t, testEnv, "test-sa", "default")

	waitForGitOpsSetCondition(t, testEnv, gs, "3 resources created")

	updated := &templatesv1.GitOpsSet{}
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), updated))

	want := []runtime.Object{
		test.MakeTestKustomization(nsn("default", "engineering-dev-demo")),
		test.MakeTestKustomization(nsn("default", "engineering-prod-demo")),
		test.MakeTestKustomization(nsn("default", "engineering-preprod-demo")),
	}
	test.AssertInventoryHasItems(t, updated, want...)
	assertKustomizationsExist(t, testEnv, "default", "engineering-dev-demo", "engineering-prod-demo", "engineering-preprod-demo")
}

func TestDefaultServiceAccountImpersonation(t *testing.T) {
	ctx := context.TODO()
	gs := makeTestGitOpsSet(t)
	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	reconciler.DefaultServiceAccount = "default-test-sa"
	defer func() {
		reconciler.DefaultServiceAccount = ""
	}()

	waitForGitOpsSetCondition(t, testEnv, gs, `failed to create Resource: kustomizations.* is forbidden: User \"system:serviceaccount:default:default-test-sa\".*`)

	// Now create a service account granting the right permissions to create
	// Kustomizations in the right namespace.
	createRBACForServiceAccount(t, testEnv, "default-test-sa", "default")

	waitForGitOpsSetCondition(t, testEnv, gs, "3 resources created")

	updated := &templatesv1.GitOpsSet{}
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), updated))

	want := []runtime.Object{
		test.MakeTestKustomization(nsn("default", "engineering-dev-demo")),
		test.MakeTestKustomization(nsn("default", "engineering-prod-demo")),
		test.MakeTestKustomization(nsn("default", "engineering-preprod-demo")),
	}

	test.AssertInventoryHasItems(t, updated, want...)
	assertKustomizationsExist(t, testEnv, "default", "engineering-dev-demo", "engineering-prod-demo", "engineering-preprod-demo")
}

func TestReconcilingUpdateOfResourcesWithServiceAccount(t *testing.T) {
	// Note that this sets up the permissions to read only
	// This asserts that we can read the resource, and update the resource.
	ctx := context.TODO()
	createRBACForServiceAccount(t, testEnv, "test-sa", "default", rbacv1.PolicyRule{
		APIGroups: []string{"kustomize.toolkit.fluxcd.io"},
		Resources: []string{"kustomizations"},
		Verbs:     []string{"get", "list", "watch", "create"},
	})

	gs := makeTestGitOpsSet(t, func(gs *templatesv1.GitOpsSet) {
		gs.Spec.ServiceAccountName = "test-sa"
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
	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetCondition(t, testEnv, gs, "1 resources created")

	// Change the role so that it can't update resources

	var role rbacv1.Role
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKey{Name: "test-role", Namespace: "default"}, &role))
	role.Rules = []rbacv1.PolicyRule{
		{
			APIGroups: []string{"kustomize.toolkit.fluxcd.io"},
			Resources: []string{"kustomizations"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
	test.AssertNoError(t, testEnv.Update(ctx, &role))

	// Update the GitOpsSet
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), gs))
	gs.Spec.Templates = []templatesv1.GitOpsSetTemplate{
		{
			Content: runtime.RawExtension{
				Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("default", "unused"), func(ks *kustomizev1.Kustomization) {
					ks.Name = "{{ .Element.cluster }}-demo"
					ks.Annotations = map[string]string{
						"testing.cluster": "{{ .Element.cluster }}",
					}
					ks.Spec.Path = "./templated/path/clusters/{{ .Element.cluster }}/"
					ks.Spec.Force = true
				})),
			},
		},
	}
	test.AssertNoError(t, testEnv.Update(ctx, gs))

	waitForGitOpsSetCondition(t, testEnv, gs, `update Resource: kustomizations.* is forbidden: User "system:serviceaccount:default:test-sa"`)

	updated := &templatesv1.GitOpsSet{}
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), updated))

	wantUpdated := test.MakeTestKustomization(nsn("default", "engineering-dev-demo"), func(k *kustomizev1.Kustomization) {
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
	test.AssertInventoryHasItems(t, updated, want...)

	kustomization := &unstructured.Unstructured{}
	kustomization.SetGroupVersionKind(kustomizationGVK)
	if err := testEnv.Get(ctx, client.ObjectKeyFromObject(wantUpdated), kustomization); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(test.ToUnstructured(t, wantUpdated), kustomization, objectMetaIgnore); diff != "" {
		t.Fatalf("failed to update Kustomization:\n%s", diff)
	}

	// Update the role to allow deletion so that the resources can be cleaned up
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKey{Name: "test-role", Namespace: "default"}, &role))
	role.Rules = []rbacv1.PolicyRule{
		{
			APIGroups: []string{"kustomize.toolkit.fluxcd.io"},
			Resources: []string{"kustomizations"},
			Verbs:     []string{"get", "list", "watch", "delete"},
		},
	}
	test.AssertNoError(t, testEnv.Update(ctx, &role))
}

// TODO: test with removed file from the inventory

func TestReconcilingWithAnnotationTriggeredReconciliation(t *testing.T) {
	ctx := context.TODO()
	gs := makeTestGitOpsSet(t)
	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetCondition(t, testEnv, gs, "3 resources created")

	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), gs))
	gs.ObjectMeta.Annotations = map[string]string{
		meta.ReconcileRequestAnnotation: time.Now().Format(time.RFC3339Nano),
	}
	test.AssertNoError(t, testEnv.Update(ctx, gs))

	g := gomega.NewWithT(t)
	g.Eventually(func() bool {
		updated := &templatesv1.GitOpsSet{}
		test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), updated))

		return updated.Status.ReconcileRequestStatus.LastHandledReconcileAt != ""
	}, timeout).Should(gomega.BeTrue())
}

func TestReconcilingNewCluster(t *testing.T) {
	ctx := context.TODO()

	// Create a new GitopsCluster object and ensure it is created
	gc := makeTestgitopsCluster(nsn("default", "test-gc"), func(g *clustersv1.GitopsCluster) {
		g.ObjectMeta.Labels = map[string]string{
			"env":  "dev",
			"team": "engineering",
		}
	})

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, gc)))
	defer deleteObject(t, testEnv, gc)

	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-set",
			Namespace: "default",
		},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{
				{
					Cluster: &templatesv1.ClusterGenerator{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"env":  "dev",
								"team": "engineering",
							},
						},
					},
				},
			},

			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("default", "go-demo"), func(ks *kustomizev1.Kustomization) {
							ks.Name = "{{ .Element.ClusterName }}-demo"
							ks.Labels = map[string]string{
								"app.kubernetes.io/instance": "{{ .Element.ClusterName }}",
								"com.example/team":           "{{ .Element.ClusterLabels.team }}",
							}
							ks.Spec.Path = "./examples/kustomize/environments/{{ .Element.ClusterLabels.env }}"
							ks.Spec.Force = true
						},
						)),
					},
				},
			},
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	g := gomega.NewWithT(t)
	g.Eventually(func() bool {
		updated := &templatesv1.GitOpsSet{}
		if err := testEnv.Get(ctx, client.ObjectKeyFromObject(gs), updated); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(updated.Status.Conditions, meta.ReadyCondition)
		if cond == nil {
			return false
		}

		return cond.Message == "1 resources created"
	}, timeout).Should(gomega.BeTrue())

	// Create a second GitopsCluster object and ensure it is created, then check the status of the GitOpsSet
	gc2 := makeTestgitopsCluster(nsn("default", "test-gc2"), func(g *clustersv1.GitopsCluster) {
		g.ObjectMeta.Labels = map[string]string{
			"env":  "dev",
			"team": "engineering",
		}
	})

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, gc2)))
	defer deleteObject(t, testEnv, gc2)

	g.Eventually(func() bool {
		updated := &templatesv1.GitOpsSet{}
		if err := testEnv.Get(ctx, client.ObjectKeyFromObject(gs), updated); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(updated.Status.Conditions, meta.ReadyCondition)
		if cond == nil {
			return false
		}

		return cond.Message == "2 resources created"
	}, timeout).Should(gomega.BeTrue())
}

func TestGenerateNamespace(t *testing.T) {
	ctx := context.TODO()

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
							{Raw: []byte(`{"team": "engineering-prod"}`)},
							{Raw: []byte(`{"team": "engineering-preprod"}`)},
						},
					},
				},
			},
			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, makeTestNamespace("{{ .Element.team }}-ns")),
					},
				},
			},
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	updated := &templatesv1.GitOpsSet{}
	g := gomega.NewWithT(t)
	g.Eventually(func() bool {
		if err := testEnv.Get(ctx, client.ObjectKeyFromObject(gs), updated); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(updated.Status.Conditions, meta.ReadyCondition)
		if cond == nil {
			return false
		}

		t.Log(updated.Status.Inventory)

		return cond.Message == "2 resources created"
	}, timeout).Should(gomega.BeTrue())

	want := []runtime.Object{
		makeTestNamespace("engineering-prod-ns"),
		makeTestNamespace("engineering-preprod-ns"),
	}
	test.AssertInventoryHasItems(t, updated, want...)
}

func makeTestgitopsCluster(name types.NamespacedName, opts ...func(*clustersv1.GitopsCluster)) *clustersv1.GitopsCluster {
	gc := &clustersv1.GitopsCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GitopsCluster",
			APIVersion: "gitops.weave.works/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
	}

	for _, opt := range opts {
		opt(gc)
	}

	return gc
}

func makeTestNamespace(name string, opts ...func(*corev1.Namespace)) *corev1.Namespace {
	n := corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	for _, o := range opts {
		o(&n)
	}

	return &n
}

func makeTestGitOpsSet(t *testing.T, opts ...func(*templatesv1.GitOpsSet)) *templatesv1.GitOpsSet {
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

func makeTestGitRepository(t *testing.T, archiveURL string) *sourcev1.GitRepository {
	gr := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-git-repo",
			Namespace: "default",
		},
		Spec: sourcev1.GitRepositorySpec{
			URL: archiveURL,
		},
	}

	return gr
}

var objectMetaIgnore = func() cmp.Option {
	metaFields := []string{"uid", "resourceVersion", "generation", "creationTimestamp", "managedFields", "status", "ownerReferences"}
	return cmpopts.IgnoreMapEntries(func(k, v any) bool {
		for _, key := range metaFields {
			if key == k {
				return true
			}
		}

		return false
	})
}()

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

func waitForGitOpsSetCondition(t *testing.T, k8sClient client.Client, gs *templatesv1.GitOpsSet, message string) {
	t.Helper()
	g := gomega.NewWithT(t)
	g.Eventually(func() bool {
		updated := &templatesv1.GitOpsSet{}
		if err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(gs), updated); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(updated.Status.Conditions, meta.ReadyCondition)
		if cond == nil {
			return false
		}

		match, err := regexp.MatchString(message, cond.Message)
		if err != nil {
			t.Fatal(err)
		}

		if !match {
			t.Logf("failed to match %q to %q", message, cond.Message)
		}
		return match
	}, timeout).Should(gomega.BeTrue())
}

func waitForGitOpsSetInventory(t *testing.T, k8sClient client.Client, gs *templatesv1.GitOpsSet, objs ...runtime.Object) {
	t.Helper()
	g := gomega.NewWithT(t)
	g.Eventually(func() bool {
		updated := &templatesv1.GitOpsSet{}
		if err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(gs), updated); err != nil {
			return false
		}

		if updated.Status.Inventory == nil {
			return false
		}

		if l := len(updated.Status.Inventory.Entries); l != len(objs) {
			t.Errorf("expected %d items, got %v", len(objs), l)
		}

		want := generateResourceInventory(objs)

		return cmp.Diff(want, updated.Status.Inventory) == ""
	}, timeout).Should(gomega.BeTrue())
}

// generateResourceInventory generates a ResourceInventory object from a list of runtime objects.
func generateResourceInventory(objs []runtime.Object) *templatesv1.ResourceInventory {
	entries := []templatesv1.ResourceRef{}
	for _, obj := range objs {
		ref, err := templatesv1.ResourceRefFromObject(obj)
		if err != nil {
			panic(err)
		}
		entries = append(entries, ref)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})

	return &templatesv1.ResourceInventory{Entries: entries}
}

func getGitRepoStatusUpdate(t *testing.T, url, checksum string) sourcev1.GitRepositoryStatus {
	return sourcev1.GitRepositoryStatus{
		Artifact: &sourcev1.Artifact{
			URL:      url,
			Checksum: checksum,
		},
	}
}

func deleteGitOpsSetAndWaitForNotFound(t *testing.T, cl client.Client, gs *templatesv1.GitOpsSet) {
	t.Helper()
	deleteObject(t, cl, gs)

	g := gomega.NewWithT(t)
	g.Eventually(func() bool {
		updated := &templatesv1.GitOpsSet{}
		return apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(gs), updated))
	}, timeout).Should(gomega.BeTrue())
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

func deleteObject(t *testing.T, cl client.Client, obj client.Object) {
	t.Helper()
	if err := cl.Delete(context.TODO(), obj); err != nil {
		t.Fatal(err)
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

func assertKustomizationDoesNotExist(t *testing.T, cl client.Client, ks *kustomizev1.Kustomization) {
	t.Helper()
	check := &unstructured.Unstructured{}
	check.SetGroupVersionKind(kustomizationGVK)

	if err := cl.Get(context.TODO(), client.ObjectKeyFromObject(ks), check); !apierrors.IsNotFound(err) {
		t.Fatalf("object %v still exists", ks)
	}
}
