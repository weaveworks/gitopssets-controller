package list

import (
	"context"
	"testing"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/gitops-tools/gitopssets-controller/test"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

var _ generators.Generator = (*ListGenerator)(nil)

func TestGenerate_with_no_lists(t *testing.T) {
	gen := GeneratorFactory(logr.Discard(), nil)
	got, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{}, nil)

	if err != nil {
		t.Errorf("got an error with no list: %s", err)
	}
	if got != nil {
		t.Errorf("got %v, want %v with no List generator", got, nil)
	}
}

func TestGenerate(t *testing.T) {
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

			gen := GeneratorFactory(logr.Discard(), nil)
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

func TestGenerate_errors(t *testing.T) {
	testCases := []struct {
		name      string
		generator *templatesv1.GitOpsSetGenerator
		wantErr   string
	}{
		{
			name: "bad json",
			generator: &templatesv1.GitOpsSetGenerator{
				List: &templatesv1.ListGenerator{
					Elements: []apiextensionsv1.JSON{{Raw: []byte(`{`)}},
				},
			},
			wantErr: "error unmarshaling list element: unexpected end of JSON input",
		},
		{
			name:      "no generator",
			generator: nil,
			wantErr:   "GitOpsSet is empty",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {

			gen := GeneratorFactory(logr.Discard(), nil)
			_, err := gen.Generate(context.TODO(), tt.generator, nil)

			test.AssertErrorMatch(t, tt.wantErr, err)
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
