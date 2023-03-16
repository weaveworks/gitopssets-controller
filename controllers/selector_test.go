package controllers

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
