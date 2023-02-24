package matrix

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/go-logr/logr"
	"github.com/imdario/mergo"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MatrixGenerator is a generator that combines the results of multiple
// generators into a single set of values.
type MatrixGenerator struct {
	client.Client
	logr.Logger
	generatorsMap map[string]generators.GeneratorFactory
}

// GeneratorFactory is a function for creating per-reconciliation generators for
// the MatrixGenerator.
func GeneratorFactory(generatorsMap map[string]generators.GeneratorFactory) generators.GeneratorFactory {
	return func(l logr.Logger, c client.Client) generators.Generator {
		return NewGenerator(l, c, generatorsMap)
	}
}

// NewGenerator creates and returns a new matrix generator.
func NewGenerator(l logr.Logger, c client.Client, g map[string]generators.GeneratorFactory) *MatrixGenerator {
	return &MatrixGenerator{
		Client:        c,
		Logger:        l,
		generatorsMap: g,
	}
}

func (mg *MatrixGenerator) Generate(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	if sg == nil {
		return nil, generators.ErrEmptyGitOpsSet
	}

	if sg.Matrix == nil {
		return nil, nil
	}

	if len(sg.Matrix.Generators) != 2 {
		return nil, generators.ErrIncorrectNumberOfGenerators
	}

	allGenerators := map[string]generators.Generator{}

	for name, factory := range mg.generatorsMap {
		g := factory(mg.Logger, mg.Client)
		allGenerators[name] = g
	}

	generated, err := generate(ctx, *sg, allGenerators, ks)
	if err != nil {
		return nil, err
	}

	if len(generated) != 2 {
		return nil, fmt.Errorf("invalid generated values, expected 2 generators, got %d", len(generated))
	}

	// Create cartesian product of results
	cartesianProduct, err := cartesian(generated[0], generated[1])
	if err != nil {
		return nil, fmt.Errorf("failed to create cartesian product of generators: %w", err)
	}

	return cartesianProduct, nil
}

// Interval is an implementation of the Generator interface.
func (g *MatrixGenerator) Interval(sg *templatesv1.GitOpsSetGenerator) time.Duration {
	allGenerators := map[string]generators.Generator{}

	for name, factory := range g.generatorsMap {
		g := factory(g.Logger, g.Client)
		allGenerators[name] = g
	}

	res := []time.Duration{}
	for _, mg := range sg.Matrix.Generators {
		relevantGenerators := generators.FindRelevantGenerators(mg, allGenerators)

		for _, rg := range relevantGenerators {
			gs, err := makeGitOpsSetGenerator(&mg)
			if err != nil {
				g.Logger.Error(err, "failed to calculate requeue interval, defaulting to no requeue")
				return generators.NoRequeueInterval
			}

			d := rg.Interval(gs)

			if d > generators.NoRequeueInterval {
				res = append(res, d)
			}

		}
	}

	if len(res) == 0 {
		return generators.NoRequeueInterval
	}

	// Find the lowest requeue interval provided by a generator.
	sort.Slice(res, func(i, j int) bool { return res[i] < res[j] })

	return res[0]
}

// generate generates the parameters for the matrix generator.
func generate(ctx context.Context, generator templatesv1.GitOpsSetGenerator, allGenerators map[string]generators.Generator, gitopsSet *templatesv1.GitOpsSet) ([][]map[string]any, error) {
	generated := [][]map[string]any{}

	for _, mg := range generator.Matrix.Generators {
		relevantGenerators := generators.FindRelevantGenerators(mg, allGenerators)
		for _, g := range relevantGenerators {
			gs, err := makeGitOpsSetGenerator(&mg)
			if err != nil {
				return nil, err
			}

			res, err := g.Generate(ctx, gs, gitopsSet)
			if err != nil {
				return nil, err
			}

			generated = append(generated, res)
		}
	}

	return generated, nil
}

// makeGitOpsSetGenerator converts a GitOpsSetNestedGenerator struct to a GitOpsSetGenerator struct.
// This is needed because MatrixGenerator includes GitOpsSetNestedGenerator struct,
// but the Generate function of the Generator interface expects a GitOpsSetGenerator struct.
func makeGitOpsSetGenerator(mg *templatesv1.GitOpsSetNestedGenerator) (*templatesv1.GitOpsSetGenerator, error) {
	mgJSON, err := json.Marshal(mg)
	if err != nil {
		return nil, err
	}

	var gs templatesv1.GitOpsSetGenerator

	if err = json.Unmarshal(mgJSON, &gs); err != nil {
		return nil, err
	}

	return &gs, nil
}

// cartesian returns the cartesian product of the two slices.
func cartesian(slice1, slice2 []map[string]any) ([]map[string]any, error) {
	var result []map[string]any

	for _, item1 := range slice1 {
		for _, item2 := range slice2 {
			newMap := make(map[string]any)
			if err := mergo.Merge(&newMap, item1, mergo.WithOverride); err != nil {
				return nil, fmt.Errorf("failed to merge maps: %w", err)
			}

			if err := mergo.Merge(&newMap, item2, mergo.WithOverride); err != nil {
				return nil, fmt.Errorf("failed to merge maps: %w", err)
			}

			// check if the result already exists
			if !alreadyExists(newMap, result) {
				result = append(result, newMap)
			}
		}
	}

	return result, nil
}

// alreadyExists checks if the newMap already exists in the result slice.
func alreadyExists(newMap map[string]any, result []map[string]any) bool {
	for _, item := range result {
		if reflect.DeepEqual(item, newMap) {
			return true
		}
	}

	return false
}
