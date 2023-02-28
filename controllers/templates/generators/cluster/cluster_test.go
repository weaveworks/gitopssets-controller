package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
)

func TestClusterGenerator_Generate(t *testing.T) {
	tests := []struct {
		name        string
		sg          *templatesv1.GitOpsSetGenerator
		clusters    []runtime.Object
		wantParams  []map[string]any
		wantErr     bool
		errContains string
	}{
		{
			name:        "return error if sg is nil",
			sg:          nil,
			wantErr:     true,
			errContains: generators.ErrEmptyGitOpsSet.Error(),
		},
		{
			name:       "return nil if sg.Cluster is nil",
			sg:         &templatesv1.GitOpsSetGenerator{},
			wantParams: nil,
		},
		{
			name: "successful clusters listing",
			sg: &templatesv1.GitOpsSetGenerator{
				Cluster: &templatesv1.ClusterGenerator{
					Selector: *metav1.AddLabelToSelector(&metav1.LabelSelector{}, "foo", "bar"),
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
					"ClusterName":        "cluster2",
					"ClusterNamespace":   "ns2",
					"ClusterLabels":      map[string]string{"foo": "bar"},
					"ClusterAnnotations": map[string]string{"key1": "value1", "key2": "value2"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newFakeClient(t, tt.clusters...)
			g := NewGenerator(logr.Discard(), c)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			gotParams, err := g.Generate(ctx, tt.sg, nil)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantParams, gotParams)
		})
	}
}

func newFakeClient(t *testing.T, objs ...runtime.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clustersv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
}
