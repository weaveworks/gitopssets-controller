package setup

import (
	"fmt"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"

	"github.com/gitops-tools/gitopssets-controller/test"
)

func TestNewSchemeForGenerators(t *testing.T) {
	tests := []struct {
		enabled  []string
		want     []schema.GroupVersionKind
		excluded []schema.GroupVersionKind
	}{
		{
			enabled: DefaultGenerators,
			want: []schema.GroupVersionKind{
				sourcev1.GroupVersion.WithKind("GitRepository"),
				templatesv1.GroupVersion.WithKind("GitOpsSet"),
			},
			excluded: []schema.GroupVersionKind{
				clustersv1.GroupVersion.WithKind("GitopsCluster"),
				imagev1.GroupVersion.WithKind("ImagePolicy"),
			},
		},
		{
			enabled: AllGenerators,
			want: []schema.GroupVersionKind{
				sourcev1.GroupVersion.WithKind("GitRepository"),
				templatesv1.GroupVersion.WithKind("GitOpsSet"),
				clustersv1.GroupVersion.WithKind("GitopsCluster"),
				imagev1.GroupVersion.WithKind("ImagePolicy"),
			},
		},
		{
			enabled: []string{"Cluster"},
			want: []schema.GroupVersionKind{
				sourcev1.GroupVersion.WithKind("GitRepository"),
				templatesv1.GroupVersion.WithKind("GitOpsSet"),
				clustersv1.GroupVersion.WithKind("GitopsCluster"),
			},
			excluded: []schema.GroupVersionKind{
				imagev1.GroupVersion.WithKind("ImagePolicy"),
			},
		},
		{
			enabled: []string{"ImagePolicy"},
			want: []schema.GroupVersionKind{
				sourcev1.GroupVersion.WithKind("GitRepository"),
				templatesv1.GroupVersion.WithKind("GitOpsSet"),
				imagev1.GroupVersion.WithKind("ImagePolicy"),
			},
			excluded: []schema.GroupVersionKind{
				clustersv1.GroupVersion.WithKind("GitopsCluster"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.enabled), func(t *testing.T) {
			scheme, err := NewSchemeForGenerators(tt.enabled)
			test.AssertNoError(t, err)

			assertContainsTypes(t, tt.want, scheme)
			refuteContainsTypes(t, tt.excluded, scheme)
		})
	}
}

func assertContainsTypes(t *testing.T, want []schema.GroupVersionKind, scheme *runtime.Scheme) {
	known := scheme.AllKnownTypes()
	for _, v := range want {
		_, ok := known[v]
		if !ok {
			t.Errorf("failed to find %v in known types", v)
		}
	}
}

func refuteContainsTypes(t *testing.T, exclude []schema.GroupVersionKind, scheme *runtime.Scheme) {
	known := scheme.AllKnownTypes()
	for _, v := range exclude {
		_, ok := known[v]
		if ok {
			t.Errorf("%v should be excluded from known types", v)
		}
	}
}

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
			`invalid generator "foo". valid values: \["GitRepository" "OCIRepository" "Cluster" "PullRequests" "List" "APIClient" "ImagePolicy" "Matrix" "Config"\]`,
		},
		{
			"case insensitive generators",
			[]string{"cluster", "List"},
			`invalid generator "cluster". valid values: \["GitRepository" "OCIRepository" "Cluster" "PullRequests" "List" "APIClient" "ImagePolicy" "Matrix" "Config"\]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEnabledGenerators(tt.enabledGenerators)
			test.AssertErrorMatch(t, tt.expected, err)
		})
	}
}
