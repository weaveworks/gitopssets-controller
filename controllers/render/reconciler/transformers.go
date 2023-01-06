package reconciler

import (
	"context"
	"reflect"

	templatesv1 "github.com/weaveworks/gitops-sets-controller/api/v1alpha1"
	"github.com/weaveworks/gitops-sets-controller/controllers/render/generators"
)

type transformResult struct {
	Params   []map[string]any
	Template templatesv1.GitOpsSetTemplate
}

func transform(ctx context.Context, generator templatesv1.GitOpsSetGenerator, allGenerators map[string]generators.Generator, template templatesv1.GitOpsSetTemplate, gitopsSet *templatesv1.GitOpsSet) ([]transformResult, error) {
	res := []transformResult{}
	generators := findRelevantGenerators(&generator, allGenerators)
	for _, g := range generators {
		params, err := g.Generate(ctx, &generator, gitopsSet)
		if err != nil {
			return nil, err
		}

		res = append(res, transformResult{
			Params:   params,
			Template: template,
		})
	}
	return res, nil
}

func findRelevantGenerators(setGenerator *templatesv1.GitOpsSetGenerator, allGenerators map[string]generators.Generator) []generators.Generator {
	var res []generators.Generator
	v := reflect.Indirect(reflect.ValueOf(setGenerator))
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.CanInterface() {
			continue
		}

		if !reflect.ValueOf(field.Interface()).IsNil() {
			res = append(res, allGenerators[v.Type().Field(i).Name])
		}
	}
	return res
}
