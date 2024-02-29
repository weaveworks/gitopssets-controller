package generators_test

import (
	"errors"
	"reflect"
	"testing"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators/list"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators/matrix"
	"github.com/gitops-tools/gitopssets-controller/test"
)

func TestFindRelevantGenerators(t *testing.T) {
	allGenerators := map[string]generators.Generator{
		"List":   &list.ListGenerator{},
		"Matrix": &matrix.MatrixGenerator{},
	}

	tests := []struct {
		name string
		set  templatesv1.GitOpsSetGenerator
		want []generators.Generator
	}{
		{
			name: "empty set",
			set:  templatesv1.GitOpsSetGenerator{},
			want: []generators.Generator{},
		},
		{
			name: "one generator",
			set: templatesv1.GitOpsSetGenerator{
				List: &templatesv1.ListGenerator{},
			},
			want: []generators.Generator{
				&list.ListGenerator{},
			},
		},
		{
			name: "two generators",
			set: templatesv1.GitOpsSetGenerator{
				List: &templatesv1.ListGenerator{},
				Matrix: &templatesv1.MatrixGenerator{
					Generators: []templatesv1.GitOpsSetNestedGenerator{
						{
							List: &templatesv1.ListGenerator{},
						},
						{
							List: &templatesv1.ListGenerator{},
						},
					},
				},
			},
			want: []generators.Generator{
				&list.ListGenerator{},
				&matrix.MatrixGenerator{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := generators.FindRelevantGenerators(tt.set, allGenerators); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindRelevantGenerators() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindRelevenatGeneratorsErrors(t *testing.T) {
	tests := []struct {
		name          string
		allGenerators map[string]generators.Generator
		set           templatesv1.GitOpsSetGenerator
		err           string
	}{
		{
			name: "unknown generator",
			allGenerators: map[string]generators.Generator{
				"List": &list.ListGenerator{},
			},
			set: templatesv1.GitOpsSetGenerator{
				List:   &templatesv1.ListGenerator{},
				Matrix: &templatesv1.MatrixGenerator{},
			},
			err: "Matrix not enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := generators.FindRelevantGenerators(tt.set, tt.allGenerators)
			test.AssertErrorMatch(t, tt.err, err)
			if !errors.As(err, &generators.GeneratorNotEnabledError{}) {
				t.Errorf("FindRelevantGenerators() error should be a GeneratorNotEnabledError")
			}
			if !errors.Is(err, generators.GeneratorNotEnabledError{Name: "Matrix"}) {
				t.Errorf(`FindRelevantGenerators() error should be GeneratorNotEnabledError{Name: "Matrix"}`)
			}
		})
	}

}
