package controllers

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
)

func TestGitOpsClusterWatchUsesClusterGeneratorIndex(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := clustersv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	matching := gitOpsSetWithClusterSelector("matching", "env", "prod")
	other := gitOpsSetWithClusterSelector("other", "env", "dev")
	empty := gitOpsSetWithClusterSelector("empty", "", "")
	listOnly := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: "list-only", Namespace: "demo"},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{{
				List: &templatesv1.ListGenerator{},
			}},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&templatesv1.GitOpsSet{}, clusterGeneratorIndexKey, indexClusterGenerators).
		WithRuntimeObjects(matching, other, empty, listOnly).
		Build()
	reconciler := &GitOpsSetReconciler{Client: cl}
	cluster := &clustersv1.GitopsCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prod-cluster",
			Namespace: "demo",
			Labels:    map[string]string{"env": "prod"},
		},
	}

	requests := reconciler.gitOpsClusterToGitOpsSet(t.Context(), cluster)
	if len(requests) != 1 || requests[0].Name != "matching" {
		t.Fatalf("requests = %#v, want only matching", requests)
	}
	if got := indexClusterGenerators(listOnly); got != nil {
		t.Fatalf("list-only index = %v, want nil", got)
	}
}

func TestGitOpsClusterWatchLogsListErrors(t *testing.T) {
	sink := &captureSink{}
	ctx := log.IntoContext(t.Context(), logr.New(sink))
	reconciler := &GitOpsSetReconciler{Client: failListClient{}}
	cluster := &clustersv1.GitopsCluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "demo"}}

	if got := reconciler.gitOpsClusterToGitOpsSet(ctx, cluster); got != nil {
		t.Fatalf("requests = %#v, want nil", got)
	}
	if len(sink.errors) != 1 || sink.errors[0] != "failed to list GitOpsSets for GitopsCluster" {
		t.Fatalf("logs = %#v", sink.errors)
	}
}

func gitOpsSetWithClusterSelector(name, key, value string) *templatesv1.GitOpsSet {
	selector := metav1.LabelSelector{}
	if key != "" {
		selector.MatchLabels = map[string]string{key: value}
	}
	return &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "demo"},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{{
				Cluster: &templatesv1.ClusterGenerator{Selector: selector},
			}},
		},
	}
}

type failListClient struct {
	client.Client
}

func (failListClient) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return fmt.Errorf("list failed")
}

type captureSink struct {
	errors []string
}

func (c *captureSink) Init(logr.RuntimeInfo)    {}
func (c *captureSink) Enabled(int) bool         { return true }
func (c *captureSink) Info(int, string, ...any) {}
func (c *captureSink) Error(_ error, msg string, _ ...any) {
	c.errors = append(c.errors, msg)
}
func (c *captureSink) WithValues(...any) logr.LogSink { return c }
func (c *captureSink) WithName(string) logr.LogSink   { return c }
