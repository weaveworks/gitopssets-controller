package matrix

import (
	"testing"

	"github.com/gitops-tools/gitopssets-controller/test"
	"github.com/google/go-cmp/cmp"
)

func TestCartesian(t *testing.T) {
	tests := map[string]struct {
		generated []generatedElements
		expected  []map[string]any
	}{
		"empty slices": {
			generated: []generatedElements{},
			expected:  []map[string]any{},
		},
		"one slice": {
			generated: []generatedElements{
				{
					elements: []map[string]any{
						{"a": 1},
						{"a": 2},
					},
				},
			},
			expected: []map[string]any{{"a": 1}, {"a": 2}},
		},
		"simple slices": {
			generated: []generatedElements{
				{
					elements: []map[string]any{
						{"eggs": 6},
						{"milk": 2},
						{"cheese": 1},
					},
				},
				{
					elements: []map[string]any{
						{"bag": 1},
					},
				},
			},
			expected: []map[string]any{
				{"eggs": 6, "bag": 1},
				{"milk": 2, "bag": 1},
				{"cheese": 1, "bag": 1},
			},
		},
		"named generators": {
			generated: []generatedElements{
				{
					name: "foods",
					elements: []map[string]any{
						{"eggs": 6},
						{"milk": 2},
						{"cheese": 1},
					},
				},
				{
					name: "carriers",
					elements: []map[string]any{
						{"bag": 1},
					},
				},
			},
			expected: []map[string]any{
				{
					"carriers": map[string]any{
						"bag": 1,
					},
					"foods": map[string]any{
						"eggs": 6,
					},
				},
				{
					"carriers": map[string]any{
						"bag": 1,
					},
					"foods": map[string]any{
						"milk": 2,
					},
				},
				{
					"carriers": map[string]any{
						"bag": 1,
					},
					"foods": map[string]any{
						"cheese": 1,
					},
				},
			},
		},
		"both slices have one element": {
			generated: []generatedElements{
				{
					elements: []map[string]any{
						{"a": 1},
					},
				},
				{
					elements: []map[string]any{
						{"b": 2},
					},
				},
			},
			expected: []map[string]any{
				{"a": 1, "b": 2},
			},
		},
		"both slices have multiple elements": {
			generated: []generatedElements{
				{
					elements: []map[string]any{
						{"a": 1},
						{"a": 2},
					},
				},
				{
					elements: []map[string]any{
						{"b": 3},
						{"b": 4},
					},
				},
			},
			expected: []map[string]any{
				{"a": 1, "b": 3},
				{"a": 1, "b": 4},
				{"a": 2, "b": 3},
				{"a": 2, "b": 4},
			},
		},
		"overlapping values and different ordering": {
			generated: []generatedElements{
				{
					elements: []map[string]any{
						{"name": "test1", "value": "value1"},
						{"name": "test2", "value": "value2"},
					},
				},
				{
					elements: []map[string]any{
						{"name": "test2", "value": "value3"},
						{"name": "test1", "value": "value4"},
					},
				},
			},
			expected: []map[string]any{
				{"name": "test2", "value": "value3"},
				{"name": "test1", "value": "value4"},
			},
		},
		"nested maps": {
			generated: []generatedElements{
				{
					elements: []map[string]any{
						{"a": 1, "b": map[string]any{"c": 2, "d": 3}},
						{"a": 4, "b": map[string]any{"c": 5, "d": 6}},
					},
				},
				{
					elements: []map[string]any{
						{"e": 7, "f": map[string]any{"g": 8, "h": 9}},
						{"e": 10, "f": map[string]any{"g": 11, "h": 12}},
					},
				},
			},
			expected: []map[string]any{
				{"a": 1, "b": map[string]any{"c": 2, "d": 3}, "e": 7, "f": map[string]any{"g": 8, "h": 9}},
				{"a": 1, "b": map[string]any{"c": 2, "d": 3}, "e": 10, "f": map[string]any{"g": 11, "h": 12}},
				{"a": 4, "b": map[string]any{"c": 5, "d": 6}, "e": 7, "f": map[string]any{"g": 8, "h": 9}},
				{"a": 4, "b": map[string]any{"c": 5, "d": 6}, "e": 10, "f": map[string]any{"g": 11, "h": 12}},
			},
		},
		"three slices": {
			generated: []generatedElements{
				{
					elements: []map[string]any{
						{"a": 1},
					},
				},
				{
					elements: []map[string]any{
						{"b": 2},
					},
				},
				{
					elements: []map[string]any{
						{"c": 3},
					},
				},
			},
			expected: []map[string]any{
				{"a": 1, "b": 2, "c": 3},
			},
		},
		"longer slices": {
			generated: []generatedElements{
				{
					elements: []map[string]any{
						{"b": 2},
					},
				},
				{
					elements: []map[string]any{
						{"a": 1},
						{"aa": 1},
						{"aaa": 1},
						{"aaaa": 1},
						{"aaaaa": 1},
					},
				},
				{
					elements: []map[string]any{
						{"c": 3},
					},
				},
			},
			expected: []map[string]any{
				{"a": 1, "b": 2, "c": 3},
				{"aa": 1, "b": 2, "c": 3},
				{"aaa": 1, "b": 2, "c": 3},
				{"aaaa": 1, "b": 2, "c": 3},
				{"aaaaa": 1, "b": 2, "c": 3},
			},
		},
		"real-world example": {
			generated: []generatedElements{
				{
					name: "staging",
					elements: []map[string]any{
						{"ClusterAnnotations": map[string]string{}, "ClusterLabels": map[string]string{"env": "staging"}, "ClusterName": "staging-cluster1", "ClusterNamespace": "clusters"},
						{"ClusterAnnotations": map[string]string{}, "ClusterLabels": map[string]string{"env": "staging"}, "ClusterName": "staging-cluster2", "ClusterNamespace": "clusters"},
					},
				},
				{
					name: "production",
					elements: []map[string]any{
						{"ClusterAnnotations": map[string]string{}, "ClusterLabels": map[string]string{"env": "production"}, "ClusterName": "production-cluster1", "ClusterNamespace": "clusters"},
						{"ClusterAnnotations": map[string]string{}, "ClusterLabels": map[string]string{"env": "production"}, "ClusterName": "production-cluster2", "ClusterNamespace": "clusters"},
					},
				},
			},
			expected: []map[string]any{
				{
					"production": map[string]any{
						"ClusterAnnotations": map[string]string{},
						"ClusterLabels":      map[string]string{"env": "production"},
						"ClusterName":        "production-cluster1",
						"ClusterNamespace":   "clusters",
					},
					"staging": map[string]any{
						"ClusterAnnotations": map[string]string{},
						"ClusterLabels":      map[string]string{"env": "staging"},
						"ClusterName":        "staging-cluster1",
						"ClusterNamespace":   "clusters",
					},
				},
				{
					"production": map[string]any{
						"ClusterAnnotations": map[string]string{},
						"ClusterLabels":      map[string]string{"env": "production"},
						"ClusterName":        "production-cluster2",
						"ClusterNamespace":   "clusters",
					},
					"staging": map[string]any{
						"ClusterAnnotations": map[string]string{},
						"ClusterLabels":      map[string]string{"env": "staging"},
						"ClusterName":        "staging-cluster1",
						"ClusterNamespace":   "clusters",
					},
				},
				{
					"production": map[string]any{
						"ClusterAnnotations": map[string]string{},
						"ClusterLabels":      map[string]string{"env": "production"},
						"ClusterName":        "production-cluster1",
						"ClusterNamespace":   "clusters",
					},
					"staging": map[string]any{
						"ClusterAnnotations": map[string]string{},
						"ClusterLabels":      map[string]string{"env": "staging"},
						"ClusterName":        "staging-cluster2",
						"ClusterNamespace":   "clusters",
					},
				},
				{
					"production": map[string]any{
						"ClusterAnnotations": map[string]string{},
						"ClusterLabels":      map[string]string{"env": "production"},
						"ClusterName":        "production-cluster2",
						"ClusterNamespace":   "clusters",
					},
					"staging": map[string]any{
						"ClusterAnnotations": map[string]string{},
						"ClusterLabels":      map[string]string{"env": "staging"},
						"ClusterName":        "staging-cluster2",
						"ClusterNamespace":   "clusters",
					},
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := cartesian(tt.generated)
			test.AssertNoError(t, err)
			if diff := cmp.Diff(tt.expected, result); diff != "" {
				t.Errorf("cartesian mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
