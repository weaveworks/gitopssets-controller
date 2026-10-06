package cluster

import (
	"testing"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
)

func TestClusterGenerator_Generate(t *testing.T) {
	tests := []struct {
		name        string
		sg          *templatesv1.GitOpsSetGenerator
		clusters    []runtime.Object
		wantParams  []map[string]any
		errContains string
	}{
		{
			name:        "return error if sg is nil",
			sg:          nil,
			errContains: generators.ErrEmptyGitOpsSet.Error(),
		},
		{
			name:       "return nil if sg.Cluster is nil",
			sg:         &templatesv1.GitOpsSetGenerator{},
			wantParams: nil,
		},
		{
			name: "generator with no label selector matches no clusters",
			sg: &templatesv1.GitOpsSetGenerator{
				Cluster: &templatesv1.ClusterGenerator{},
			},
			clusters: []runtime.Object{
				&clustersv1.GitopsCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "cluster1",
						Namespace:   "ns1",
						Annotations: map[string]string{},
						Labels:      map[string]string{"test1": "value"},
					},
					Spec: clustersv1.GitopsClusterSpec{},
				},
				&clustersv1.GitopsCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "cluster2",
						Namespace:   "ns2",
						Annotations: map[string]string{},
						Labels:      map[string]string{"test2": "value"},
					},
					Spec: clustersv1.GitopsClusterSpec{},
				},
			},
			wantParams: nil,
		},
		{
			name: "label selector filtering",
			sg: &templatesv1.GitOpsSetGenerator{
				Cluster: &templatesv1.ClusterGenerator{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"foo": "bar",
						},
					},
				},
			},
			clusters: []runtime.Object{
				&clustersv1.GitopsCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1",
						Namespace: "ns1",
					},
					Spec: clustersv1.GitopsClusterSpec{},
				},
				&clustersv1.GitopsCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "cluster2",
						Namespace:   "ns2",
						Labels:      map[string]string{"foo": "bar"},
						Annotations: map[string]string{"key1": "value1", "key2": "value2"},
					},
					Spec: clustersv1.GitopsClusterSpec{},
				},
			},
			wantParams: []map[string]any{
				{
					"ClusterName":           "cluster2",
					"ClusterNamespace":      "ns2",
					"ClusterLabels":         map[string]any{"foo": "bar"},
					"ClusterAnnotations":    map[string]any{"key1": "value1", "key2": "value2"},
					"ClusterSecretRef":      "",
					"ClusterCAPIClusterRef": "",
					"ClusterReady":          "",
					"ClusterConnected":      "",
				},
			},
		},
		{
			name: "secret, CAPI reference, and connectivity conditions",
			sg: &templatesv1.GitOpsSetGenerator{
				Cluster: &templatesv1.ClusterGenerator{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
				},
			},
			clusters: []runtime.Object{
				&clustersv1.GitopsCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prod",
						Namespace: "clusters",
						Labels:    map[string]string{"env": "prod"},
					},
					Spec: clustersv1.GitopsClusterSpec{
						SecretRef: &fluxmeta.LocalObjectReference{Name: "prod-kubeconfig"},
					},
					Status: clustersv1.GitopsClusterStatus{
						Conditions: []metav1.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"},
							{Type: clustersv1.ClusterConnectivity, Status: metav1.ConditionFalse, Reason: "ClusterConnectionFailed"},
						},
					},
				},
			},
			wantParams: []map[string]any{
				{
					"ClusterName":           "prod",
					"ClusterNamespace":      "clusters",
					"ClusterLabels":         map[string]any{"env": "prod"},
					"ClusterAnnotations":    map[string]any{},
					"ClusterSecretRef":      "prod-kubeconfig",
					"ClusterCAPIClusterRef": "",
					"ClusterReady":          "True",
					"ClusterConnected":      "False",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newFakeClient(t, tt.clusters...)
			g := NewGenerator(logr.Discard(), c)

			gotParams, err := g.Generate(t.Context(), tt.sg, nil)

			if tt.errContains != "" {
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantParams, gotParams)
		})
	}
}

func newFakeClient(t *testing.T, objs ...runtime.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	assert.NoError(t, clustersv1.AddToScheme(scheme))
	assert.NoError(t, templatesv1.AddToScheme(scheme))

	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
}
