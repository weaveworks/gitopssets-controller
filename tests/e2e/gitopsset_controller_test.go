package tests

import (
	"context"
	"encoding/json"
	"testing"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta2"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/test"

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
	gc := makeTestgitopsCluster(nsn("default", "test-gc"), func(g *clustersv1.GitopsCluster) {
		g.ObjectMeta.Labels = map[string]string{
			"env":  "dev",
			"team": "engineering",
		}
	})

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, gc)))
	defer cleanupResource(t, testEnv, gc)

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
	defer cleanupResource(t, testEnv, gs)
	defer deleteAllKustomizations(t, testEnv)

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
	defer cleanupResource(t, testEnv, gc2)

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
	defer cleanupResource(t, testEnv, gs)

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
	// Namespaces cannot be deleted from envtest
	// https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
	// https://github.com/kubernetes-sigs/controller-runtime/issues/880

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

func TestEventsWithReconciling(t *testing.T) {
	eventRecorder.Reset()
	ctx := context.TODO()

	// Create a new GitopsCluster object and ensure it is created
	gc := makeTestgitopsCluster(nsn("default", "test-gc"), func(g *clustersv1.GitopsCluster) {
		g.ObjectMeta.Labels = map[string]string{
			"env":  "dev",
			"team": "engineering",
		}
	})

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, gc)))
	defer cleanupResource(t, testEnv, gc)

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
	defer cleanupResource(t, testEnv, gs)
	defer deleteAllKustomizations(t, testEnv)

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

	// Create a new GitopsCluster object and ensure it is created
	gc := makeTestgitopsCluster(nsn("default", "test-gc"), func(g *clustersv1.GitopsCluster) {
		g.ObjectMeta.Labels = map[string]string{
			"env":  "dev",
			"team": "engineering",
		}
	})

	test.AssertNoError(t, testEnv.Create(ctx, test.ToUnstructured(t, gc)))
	defer cleanupResource(t, testEnv, gc)

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
			ServiceAccountName: "test-sa",
		},
	}

	test.AssertNoError(t, testEnv.Create(ctx, gs))
	defer cleanupResource(t, testEnv, gs)
	defer deleteAllKustomizations(t, testEnv)

	g := gomega.NewWithT(t)
	g.Eventually(func() bool {
		// reconciliation should fail because the service account does not have permissions
		want := []*test.EventData{
			{
				EventType: "Error",
				Reason:    "ReconciliationFailed",
			},
		}
		return cmp.Diff(want, eventRecorder.Events, cmpopts.IgnoreFields(test.EventData{}, "Message")) == ""

	}, timeout).Should(gomega.BeTrue())

}

func cleanupResource(t *testing.T, cl client.Client, obj client.Object) {
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

func nsn(namespace, name string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
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
