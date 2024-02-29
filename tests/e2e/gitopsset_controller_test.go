package tests

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"testing"

	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta2"
	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/test"

	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
)

var kustomizationGVK = schema.GroupVersionKind{
	Group:   "kustomize.toolkit.fluxcd.io",
	Kind:    "Kustomization",
	Version: "v1beta2",
}

func TestReconcilingNewCluster(t *testing.T) {
	ctx := context.TODO()
	// Create a new GitopsCluster object and ensure it is created
	gc := makeTestGitopsCluster(nsn("default", "test-gc"), func(g *clustersv1.GitopsCluster) {
		g.ObjectMeta.Labels = map[string]string{
			"env":                 "dev",
			"team":                "engineering",
			"example.com/testing": "tricky-label",
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
								"com.example/new":            `{{ get .Element.ClusterLabels "example.com/testing" }}`,
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

	waitForGitOpsSetCondition(t, testEnv, gs, "1 resources created")

	// Create a second GitopsCluster object and ensure it is created, then check the status of the GitOpsSet
	gc2 := makeTestGitopsCluster(nsn("default", "test-gc2"), func(g *clustersv1.GitopsCluster) {
		g.ObjectMeta.Labels = map[string]string{
			"env":  "dev",
			"team": "engineering",
		}
	})

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, gc2)))
	defer deleteObject(t, testEnv, gc2)

	waitForGitOpsSetCondition(t, testEnv, gs, "2 resources created")

	var kust kustomizev1.Kustomization
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKey{Name: "test-gc-demo", Namespace: "default"}, &kust))

	wantLabels := map[string]string{
		"app.kubernetes.io/instance": "test-gc",
		"com.example/new":            "tricky-label",
		"com.example/team":           "engineering",
		"sets.gitops.pro/name":       "demo-set",
		"sets.gitops.pro/namespace":  "default",
	}
	if diff := cmp.Diff(wantLabels, kust.ObjectMeta.Labels); diff != "" {
		t.Fatalf("failed to generate labels:\n%s", diff)
	}
}

func TestReconcilingPartialApply(t *testing.T) {
	ctx := context.TODO()

	prodCM := test.NewConfigMap(func(c *corev1.ConfigMap) {
		c.SetName("engineering-prod-cm")
		c.Data = map[string]string{
			"testing": "testing-element",
		}
	})
	test.AssertNoError(t, testEnv.Create(ctx, prodCM))
	defer deleteObject(t, testEnv, prodCM)

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
							{Raw: []byte(`{"name": "engineering-prod"}`)},
							{Raw: []byte(`{"name": "engineering-dev"}`)},
						},
					},
				},
			},
			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.NewConfigMap(func(c *corev1.ConfigMap) {
							c.SetName("{{ .Element.name }}-cm")

							c.Data = map[string]string{
								"testing": "{{ .Element.name }}",
							}
						})),
					},
				},
			},
		},
	}
	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetCondition(t, testEnv, gs, `failed to create Resource: configmaps \"engineering-prod-cm\" already exists`)

	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), gs))
	if l := len(gs.Status.Inventory.Entries); l != 1 {
		t.Errorf("didn't record created resources correctly, got %d, want 1", l)
	}

	var cm corev1.ConfigMap
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKey{Name: "engineering-dev-cm", Namespace: "default"}, &cm))

	want := map[string]string{
		"testing": "engineering-dev",
	}
	if diff := cmp.Diff(want, cm.Data); diff != "" {
		t.Fatalf("failed to generate ConfigMap:\n%s", diff)
	}
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
						Raw: mustMarshalJSON(t, test.NewNamespace("{{ .Element.team }}-ns")),
					},
				},
			},
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetCondition(t, testEnv, gs, "2 resources created")
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), gs))

	want := []runtime.Object{
		test.NewNamespace("engineering-prod-ns"),
		test.NewNamespace("engineering-preprod-ns"),
	}
	test.AssertInventoryHasItems(t, gs, want...)

	// Namespaces cannot be deleted from envtest
	// https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
	// https://github.com/kubernetes-sigs/controller-runtime/issues/880
}

func TestReconcilingWithAnnotationChange(t *testing.T) {
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
						Raw: mustMarshalJSON(t, test.NewConfigMap(func(c *corev1.ConfigMap) {
							c.SetName("{{ .Element.team }}-cm")

							c.Data = map[string]string{
								"testing": "{{ .Element.team }}",
							}
						})),
					},
				},
			},
		},
	}
	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetCondition(t, testEnv, gs, "2 resources created")

	var cm corev1.ConfigMap
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKey{Name: "engineering-prod-cm", Namespace: "default"}, &cm))

	want := map[string]string{
		"testing": "engineering-prod",
	}
	if diff := cmp.Diff(want, cm.Data); diff != "" {
		t.Fatalf("failed to generate ConfigMap:\n%s", diff)
	}

	test.AssertNoError(t, testEnv.Delete(ctx, &cm))

	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), gs))
	gs.Annotations = map[string]string{
		meta.ReconcileRequestAnnotation: "testing",
	}
	test.AssertNoError(t, testEnv.Update(ctx, gs))

	g := gomega.NewWithT(t)
	g.Eventually(func() bool {
		return testEnv.Get(ctx, client.ObjectKey{Name: "engineering-prod-cm", Namespace: "default"}, &cm) == nil
	}, timeout).Should(gomega.BeTrue())

	g.Eventually(func() bool {
		test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(gs), gs))
		return gs.Status.ReconcileRequestStatus.LastHandledReconcileAt != ""
	}, timeout).Should(gomega.BeTrue())
}

func TestReconcilingUpdatingImagePolicy(t *testing.T) {
	ctx := context.TODO()
	ip := test.NewImagePolicy()

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, ip)))
	defer deleteObject(t, testEnv, ip)

	ip = waitForResource[*imagev1.ImagePolicy](t, testEnv, ip)
	ip.Status.LatestImage = "testing/test:v0.30.0"
	test.AssertNoError(t, testEnv.Status().Update(ctx, ip))

	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-set",
			Namespace: "default",
		},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{
				{
					ImagePolicy: &templatesv1.ImagePolicyGenerator{
						PolicyRef: ip.GetName(),
					},
				},
			},

			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.NewConfigMap(func(c *corev1.ConfigMap) {
							c.ObjectMeta.Name = "test-configmap-{{ .ElementIndex }}"
							c.Data = map[string]string{
								"testing": "{{ .Element.latestImage }}",
							}
						})),
					},
				},
			},
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	ip = waitForResource[*imagev1.ImagePolicy](t, testEnv, ip)
	ip.Status.LatestImage = "testing/test:v0.31.0"
	test.AssertNoError(t, testEnv.Status().Update(ctx, ip))

	waitForGitOpsSetCondition(t, testEnv, gs, "1 resources created")

	var cm corev1.ConfigMap
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKey{Name: "test-configmap-0", Namespace: "default"}, &cm))

	want := map[string]string{
		"testing": "testing/test:v0.31.0",
	}
	if diff := cmp.Diff(want, cm.Data); diff != "" {
		t.Fatalf("failed to generate ConfigMap:\n%s", diff)
	}
}

func TestReconcilingUpdatingImagePolicy_in_matrix(t *testing.T) {
	ctx := context.TODO()
	ip := test.NewImagePolicy()

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, ip)))
	defer deleteObject(t, testEnv, ip)

	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(ip), ip))
	ip.Status.LatestImage = "testing/test:v0.30.0"
	test.AssertNoError(t, testEnv.Status().Update(ctx, ip))

	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-set",
			Namespace: "default",
		},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{
				{
					Matrix: &templatesv1.MatrixGenerator{
						Generators: []templatesv1.GitOpsSetNestedGenerator{
							{
								ImagePolicy: &templatesv1.ImagePolicyGenerator{
									PolicyRef: ip.GetName(),
								},
							},
							{
								List: &templatesv1.ListGenerator{
									Elements: []apiextensionsv1.JSON{
										{Raw: []byte(`{"team": "engineering-prod"}`)},
										{Raw: []byte(`{"team": "engineering-preprod"}`)},
									},
								},
							},
						},
					},
				},
			},
			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.NewConfigMap(func(c *corev1.ConfigMap) {
							c.ObjectMeta.Name = "{{ .Element.team }}-demo-cm"
							c.Data = map[string]string{
								"testing": "{{ .Element.latestImage }}",
								"team":    "{{ .Element.team }}",
							}
						})),
					},
				},
			},
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetCondition(t, testEnv, gs, "2 resources created")

	var cm corev1.ConfigMap
	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKey{Name: "engineering-preprod-demo-cm", Namespace: "default"}, &cm))

	want := map[string]string{
		"team":    "engineering-preprod",
		"testing": "testing/test:v0.30.0",
	}
	if diff := cmp.Diff(want, cm.Data); diff != "" {
		t.Fatalf("failed to generate ConfigMap:\n%s", diff)
	}
}

func TestGitOpsSetUpdateOnGitRepoChange(t *testing.T) {
	eventRecorder.Reset()
	ctx := context.TODO()

	// Create a GitRepository with a fake archive server.
	srv := test.StartFakeArchiveServer(t, "testdata/archive")
	gRepo := makeTestGitRepository(t, srv.URL+"/files.tar.gz")
	test.AssertNoError(t, testEnv.Create(ctx, gRepo))

	gRepo.Status.Artifact = newArtifact(srv.URL+"/files.tar.gz", "sha256:f0a57ec1cdebda91cf00d89dfa298c6ac27791e7fdb0329990478061755eaca8")

	test.AssertNoError(t, testEnv.Status().Update(ctx, gRepo))

	// Create a GitOpsSet that uses the GitRepository.
	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-set",
			Namespace: "default",
		},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{
				{
					Matrix: &templatesv1.MatrixGenerator{
						Generators: []templatesv1.GitOpsSetNestedGenerator{
							{
								GitRepository: &templatesv1.GitRepositoryGenerator{
									RepositoryRef: "my-git-repo",
									Files: []templatesv1.RepositoryGeneratorFileItem{
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
			},
			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("default", "eng-{{ .Element.environment }}-demo"))),
					},
				},
			},
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetInventory(t, testEnv, gs, test.MakeTestKustomization(nsn("default", "eng-dev-demo")))

	// Update the GitRepository to point to a new archive.
	gRepo.Status.Artifact = newArtifact(srv.URL+"/files-develop.tar.gz", "sha256:14cc05e5d1206860b56630b41676dbfed05533011177e123c5dd48c72b959cdc")
	test.AssertNoError(t, testEnv.Status().Update(ctx, gRepo))

	waitForGitOpsSetInventory(t, testEnv, gs, test.MakeTestKustomization(nsn("default", "eng-development-demo")))
}

func TestGitOpsSetUpdateOnOCIRepoChange(t *testing.T) {
	eventRecorder.Reset()
	ctx := context.TODO()

	// Create an OCIRepository with a fake archive server.
	srv := test.StartFakeArchiveServer(t, "testdata/archive")
	oRepo := makeTestOCIRepository(t, "oci://ghcr.io/stefanprodan/manifests/podinfo")
	test.AssertNoError(t, testEnv.Create(ctx, oRepo))

	oRepo.Status.Artifact = newArtifact(srv.URL+"/files.tar.gz", "sha256:f0a57ec1cdebda91cf00d89dfa298c6ac27791e7fdb0329990478061755eaca8")

	test.AssertNoError(t, testEnv.Status().Update(ctx, oRepo))

	// Create a GitOpsSet that uses the OCIRepository.
	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-set",
			Namespace: "default",
		},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{
				{
					Matrix: &templatesv1.MatrixGenerator{
						Generators: []templatesv1.GitOpsSetNestedGenerator{
							{
								OCIRepository: &templatesv1.OCIRepositoryGenerator{
									RepositoryRef: "my-oci-repo",
									Files: []templatesv1.RepositoryGeneratorFileItem{
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
			},
			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.MakeTestKustomization(nsn("default", "eng-{{ .Element.environment }}-demo"))),
					},
				},
			},
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	waitForGitOpsSetInventory(t, testEnv, gs, test.MakeTestKustomization(nsn("default", "eng-dev-demo")))

	// Update the OCIRepository to point to a new archive.
	oRepo.Status.Artifact = newArtifact(srv.URL+"/files-develop.tar.gz", "sha256:14cc05e5d1206860b56630b41676dbfed05533011177e123c5dd48c72b959cdc")

	test.AssertNoError(t, testEnv.Status().Update(ctx, oRepo))

	waitForGitOpsSetInventory(t, testEnv, gs, test.MakeTestKustomization(nsn("default", "eng-development-demo")))
}

func TestReconcilingUpdatingConfigMap(t *testing.T) {
	ctx := context.TODO()
	src := test.NewConfigMap(func(cm *corev1.ConfigMap) {
		cm.ObjectMeta.Name = "test-cm"
		cm.Data = map[string]string{
			"testKey": "testing",
		}
	})

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, src)))
	defer deleteObject(t, testEnv, src)

	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-set",
			Namespace: "default",
		},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{
				{
					Config: &templatesv1.ConfigGenerator{
						Name: src.GetName(),
						Kind: "ConfigMap",
					},
				},
			},

			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.NewConfigMap(func(c *corev1.ConfigMap) {
							c.Data = map[string]string{
								"testing": "{{ .Element.testKey }}",
							}
						})),
					},
				},
			},
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)
	waitForGitOpsSetCondition(t, testEnv, gs, "1 resources created")

	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(src), src))
	src.Data["testKey"] = "another-value"
	test.AssertNoError(t, testEnv.Update(ctx, src))

	want := map[string]string{
		"testing": "another-value",
	}
	waitForConfigMap(t, testEnv, client.ObjectKey{Name: "demo-cm", Namespace: "default"}, want)
}

func TestReconcilingUpdatingConfigMap_in_matrix(t *testing.T) {
	ctx := context.TODO()
	src := test.NewConfigMap(func(cm *corev1.ConfigMap) {
		cm.ObjectMeta.Name = "test-cm"
		cm.Data = map[string]string{
			"testKey": "testing",
		}
	})

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, src)))
	defer deleteObject(t, testEnv, src)

	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-set",
			Namespace: "default",
		},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{
				{
					Matrix: &templatesv1.MatrixGenerator{
						Generators: []templatesv1.GitOpsSetNestedGenerator{
							{
								Config: &templatesv1.ConfigGenerator{
									Name: src.GetName(),
									Kind: "ConfigMap",
								},
							},
							{
								List: &templatesv1.ListGenerator{
									Elements: []apiextensionsv1.JSON{
										{Raw: []byte(`{"team": "engineering-prod"}`)},
									},
								},
							},
						},
					},
				},
			},

			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.NewConfigMap(func(c *corev1.ConfigMap) {
							c.Data = map[string]string{
								"testing": "{{ .Element.testKey }}",
								"team":    "{{ .Element.team }}",
							}
						})),
					},
				},
			},
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)
	waitForGitOpsSetCondition(t, testEnv, gs, "1 resources created")

	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(src), src))
	src.Data["testKey"] = "another-value"
	test.AssertNoError(t, testEnv.Update(ctx, src))

	want := map[string]string{
		"testing": "another-value",
		"team":    "engineering-prod",
	}
	waitForConfigMap(t, testEnv, client.ObjectKey{Name: "demo-cm", Namespace: "default"}, want)
}

func TestReconcilingUpdatingSecret(t *testing.T) {
	ctx := context.TODO()
	src := test.NewSecret(func(s *corev1.Secret) {
		s.ObjectMeta.Name = "test-secret"
		s.Data = map[string][]byte{
			"testKey": []byte("testing"),
		}
	})

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, src)))
	defer deleteObject(t, testEnv, src)

	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-set",
			Namespace: "default",
		},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{
				{
					Config: &templatesv1.ConfigGenerator{
						Name: src.GetName(),
						Kind: "Secret",
					},
				},
			},

			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.NewConfigMap(func(c *corev1.ConfigMap) {
							c.Data = map[string]string{
								"testing": "{{ .Element.testKey }}",
							}
						})),
					},
				},
			},
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)
	waitForGitOpsSetCondition(t, testEnv, gs, "1 resources created")

	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(src), src))
	src.Data["testKey"] = []byte("another-value")
	test.AssertNoError(t, testEnv.Update(ctx, src))

	want := map[string]string{
		"testing": "another-value",
	}
	waitForConfigMap(t, testEnv, client.ObjectKey{Name: "demo-cm", Namespace: "default"}, want)
}

func TestReconcilingUpdatingSecret_in_matrix(t *testing.T) {
	ctx := context.TODO()
	src := test.NewConfigMap(func(cm *corev1.ConfigMap) {
		cm.ObjectMeta.Name = "test-cm"
		cm.Data = map[string]string{
			"testKey": "testing",
		}
	})

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, src)))
	defer deleteObject(t, testEnv, src)

	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-set",
			Namespace: "default",
		},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{
				{
					Matrix: &templatesv1.MatrixGenerator{
						Generators: []templatesv1.GitOpsSetNestedGenerator{
							{
								Config: &templatesv1.ConfigGenerator{
									Name: src.GetName(),
									Kind: "ConfigMap",
								},
							},
							{
								List: &templatesv1.ListGenerator{
									Elements: []apiextensionsv1.JSON{
										{Raw: []byte(`{"team": "engineering-prod"}`)},
									},
								},
							},
						},
					},
				},
			},

			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.NewConfigMap(func(c *corev1.ConfigMap) {
							c.Data = map[string]string{
								"testing": "{{ .Element.testKey }}",
								"team":    "{{ .Element.team }}",
							}
						})),
					},
				},
			},
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)
	waitForGitOpsSetCondition(t, testEnv, gs, "1 resources created")

	test.AssertNoError(t, testEnv.Get(ctx, client.ObjectKeyFromObject(src), src))
	src.Data["testKey"] = "another-value"
	test.AssertNoError(t, testEnv.Update(ctx, src))

	want := map[string]string{
		"testing": "another-value",
		"team":    "engineering-prod",
	}
	waitForConfigMap(t, testEnv, client.ObjectKey{Name: "demo-cm", Namespace: "default"}, want)
}

func waitForConfigMap(t *testing.T, k8sClient client.Client, src client.ObjectKey, want map[string]string) {
	g := gomega.NewWithT(t)
	g.Eventually(func() map[string]string {
		var cm corev1.ConfigMap
		if err := k8sClient.Get(ctx, src, &cm); err != nil {
			return nil
		}

		return cm.Data
	}, timeout).Should(gomega.Equal(want))
}

func waitForResource[T client.Object](t *testing.T, k8sClient client.Client, obj T) T {
	g := gomega.NewWithT(t)
	g.Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	}, timeout).Should(gomega.BeNil())

	return obj
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

func newArtifact(url, checksum string) *sourcev1.Artifact {
	return &sourcev1.Artifact{
		URL:            url,
		Digest:         checksum,
		LastUpdateTime: metav1.Now(),
	}
}

func TestEventsWithReconciling(t *testing.T) {
	eventRecorder.Reset()
	ctx := context.TODO()

	// Create a new GitopsCluster object and ensure it is created
	gc := makeTestGitopsCluster(nsn("default", "test-gc"), func(g *clustersv1.GitopsCluster) {
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

	want := &test.EventData{
		EventType: "Normal",
		Reason:    "ReconciliationSucceeded",
	}
	compareWant := gomega.BeComparableTo(want, cmpopts.IgnoreFields(test.EventData{}, "Message"))

	g := gomega.NewWithT(t)

	g.Eventually(func() []*test.EventData {
		return eventRecorder.Events
	}, timeout).Should(gomega.ContainElement(compareWant))
}

func TestEventsWithFailingReconciling(t *testing.T) {
	eventRecorder.Reset()
	ctx := context.TODO()

	prodCM := test.NewConfigMap(func(c *corev1.ConfigMap) {
		c.SetName("engineering-prod-cm")
		c.Data = map[string]string{
			"testing": "testing-element",
		}
	})
	test.AssertNoError(t, testEnv.Create(ctx, prodCM))
	defer deleteObject(t, testEnv, prodCM)

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
							{Raw: []byte(`{"name": "engineering-prod"}`)},
							{Raw: []byte(`{"name": "engineering-dev"}`)},
						},
					},
				},
			},
			Templates: []templatesv1.GitOpsSetTemplate{
				{
					Content: runtime.RawExtension{
						Raw: mustMarshalJSON(t, test.NewConfigMap(func(c *corev1.ConfigMap) {
							c.SetName("{{ .Element.name }}-cm")

							c.Data = map[string]string{
								"testing": "{{ .Element.name }}",
							}
						})),
					},
				},
			},
		},
	}
	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer deleteGitOpsSetAndWaitForNotFound(t, testEnv, gs)

	g := gomega.NewWithT(t)
	g.Eventually(func() bool {
		// reconciliation should fail because there is an existing resource.
		want := []*test.EventData{
			{
				EventType: corev1.EventTypeWarning,
				Reason:    "ReconciliationFailed",
			},
		}

		return cmp.Diff(want, eventRecorder.Events, cmpopts.IgnoreFields(test.EventData{}, "Message")) == ""
	}, timeout).Should(gomega.BeTrue())
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

func deleteObject(t *testing.T, cl client.Client, obj client.Object) {
	t.Helper()
	if err := cl.Delete(context.TODO(), obj); err != nil {
		t.Fatal(err)
	}
}

func mustMarshalJSON(t *testing.T, r runtime.Object) []byte {
	b, err := json.Marshal(r)
	test.AssertNoError(t, err)

	return b
}

func makeTestGitopsCluster(name types.NamespacedName, opts ...func(*clustersv1.GitopsCluster)) *clustersv1.GitopsCluster {
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

func nsn(namespace, name string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

func makeTestGitRepository(t *testing.T, archiveURL string) *sourcev1beta2.GitRepository {
	gr := &sourcev1beta2.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-git-repo",
			Namespace: "default",
		},
		Spec: sourcev1beta2.GitRepositorySpec{
			URL: archiveURL,
		},
	}

	return gr
}

func makeTestOCIRepository(t *testing.T, repoURL string) *sourcev1beta2.OCIRepository {
	gr := &sourcev1beta2.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-oci-repo",
			Namespace: "default",
		},
		Spec: sourcev1beta2.OCIRepositorySpec{
			URL: repoURL,
		},
	}

	return gr
}
