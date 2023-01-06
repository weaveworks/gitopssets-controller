package list

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	templatesv1 "github.com/weaveworks/gitops-sets-controller/api/v1alpha1"
	"github.com/weaveworks/gitops-sets-controller/controllers/render/generators"
	"github.com/weaveworks/gitops-sets-controller/test"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

var _ generators.Generator = (*ListGenerator)(nil)

func TestGenerateListParams(t *testing.T) {
	testCases := []struct {
		name     string
		elements []apiextensionsv1.JSON
		want     []map[string]any
	}{
		{
			name:     "simple key/value pairs",
			elements: []apiextensionsv1.JSON{{Raw: []byte(`{"cluster": "cluster","url": "url"}`)}},
			want:     []map[string]any{{"cluster": "cluster", "url": "url"}},
		},
		{
			name:     "nested key/values",
			elements: []apiextensionsv1.JSON{{Raw: []byte(`{"cluster": "cluster","url": "url","values":{"foo":"bar"}}`)}},
			want:     []map[string]any{{"cluster": "cluster", "url": "url", "values": map[string]any{"foo": "bar"}}},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {

			gen := NewGenerator(logr.Discard())
			got, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{
				List: &templatesv1.ListGenerator{
					Elements: tt.elements,
				},
			}, nil)

			test.AssertNoError(t, err)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("failed to generate list elements:\n%s", diff)
			}
		})
	}
}

func TestListGenerator_Interval(t *testing.T) {
	gen := NewGenerator(logr.Discard())
	sg := &templatesv1.GitOpsSetGenerator{
		List: &templatesv1.ListGenerator{
			Elements: []apiextensionsv1.JSON{{Raw: []byte(`{"cluster": "cluster","url": "url"}`)}},
		},
	}

	d := gen.Interval(sg)

	if d != generators.NoRequeueInterval {
		t.Fatalf("got %#v want %#v", d, generators.NoRequeueInterval)
	}
}
