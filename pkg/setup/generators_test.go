package setup

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/weaveworks/gitopssets-controller/test"
)

func TestGetGenerators(t *testing.T) {
	tests := []struct {
		name              string
		enabledGenerators []string
		expected          []string
	}{
		{
			"empty enabled generators",
			[]string{},
			[]string{},
		},
		{
			"enabled generators",
			[]string{"Cluster", "List"},
			[]string{"Cluster", "List"},
		},
		{
			"case is important",
			[]string{"cluster", "List"},
			[]string{"List"},
		},
		{
			"unknown enabled generators are ignored",
			[]string{"Cluster", "List", "foo"},
			[]string{"Cluster", "List"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetGenerators(tt.enabledGenerators, nil, nil)
			keys := make([]string, 0, len(got))
			for k := range got {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if cmp.Diff(tt.expected, keys) != "" {
				t.Errorf("getGenerators() = %v, want %v", keys, tt.expected)
			}
		})
	}
}

func TestValidateEnabledGenerators(t *testing.T) {
	tests := []struct {
		name              string
		enabledGenerators []string
		expected          string
	}{
		{
			"empty enabled generators",
			[]string{},
			"",
		},
		{
			"enabled generators",
			[]string{"Cluster", "List"},
			"",
		},
		{
			"unknown enabled generators raise error",
			[]string{"Cluster", "List", "foo"},
			`invalid generator "foo". valid values: \["GitRepository" "Cluster" "PullRequests" "List" "APIClient" "ImagePolicy" "Matrix"\]`,
		},
		{
			"case insensitive generators",
			[]string{"cluster", "List"},
			`invalid generator "cluster". valid values: \["GitRepository" "Cluster" "PullRequests" "List" "APIClient" "ImagePolicy" "Matrix"\]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEnabledGenerators(tt.enabledGenerators)
			test.AssertErrorMatch(t, tt.expected, err)
		})
	}
}
