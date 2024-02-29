package matrix

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	"dario.cat/mergo"
	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MatrixGenerator is a generator that combines the results of multiple
// generators into a single set of values.
type MatrixGenerator struct {
	Client client.Reader
	logr.Logger
	generatorsMap map[string]generators.GeneratorFactory
}

// GeneratorFactory is a function for creating per-reconciliation generators for
// the MatrixGenerator.
func GeneratorFactory(generatorsMap map[string]generators.GeneratorFactory) generators.GeneratorFactory {
	return func(l logr.Logger, c client.Reader) generators.Generator {
		return NewGenerator(l, c, generatorsMap)
	}
}

// NewGenerator creates and returns a new matrix generator.
func NewGenerator(l logr.Logger, c client.Reader, g map[string]generators.GeneratorFactory) *MatrixGenerator {
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

	allGenerators := map[string]generators.Generator{}

	for name, factory := range mg.generatorsMap {
		g := factory(mg.Logger, mg.Client)
		allGenerators[name] = g
	}

	generated, err := generate(ctx, *sg, allGenerators, ks)
	if err != nil {
		return nil, err
	}

	if sg.Matrix.SingleElement {
		return singleElement(generated)
	}

	product, err := cartesian(generated)
	if err != nil {
		return nil, fmt.Errorf("failed to create cartesian product of generators: %w", err)
	}

	return product, nil
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
		relevantGenerators, err := generators.FindRelevantGenerators(mg, allGenerators)
		if err != nil {
			g.Logger.Error(err, "failed to find relevant generators, defaulting to no requeue")
			return generators.NoRequeueInterval
		}

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

type generatedElements struct {
	name     string
	elements []map[string]any
}

// generate generates the parameters for the matrix generator.
func generate(ctx context.Context, generator templatesv1.GitOpsSetGenerator, allGenerators map[string]generators.Generator, gitopsSet *templatesv1.GitOpsSet) ([]generatedElements, error) {
	generated := []generatedElements{}

	for _, mg := range generator.Matrix.Generators {
		name := mg.Name
		relevantGenerators, err := generators.FindRelevantGenerators(mg, allGenerators)
		if err != nil {
			return nil, err
		}
		for _, g := range relevantGenerators {
			gs, err := makeGitOpsSetGenerator(&mg)
			if err != nil {
				return nil, err
			}

			res, err := g.Generate(ctx, gs, gitopsSet)
			if err != nil {
				return nil, err
			}

			if len(res) > 0 {
				generated = append(generated, generatedElements{name: name, elements: res})
			}
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

// cartesian returns the cartesian product of a matrix with no
// duplicates.
func cartesian(generated []generatedElements) ([]map[string]any, error) {
	if len(generated) == 0 {
		return []map[string]any{}, nil
	}

	slices := [][]map[string]any{}
	for _, res := range generated {
		if res.name != "" {
			prefixed := make([]map[string]any, len(res.elements))
			for i, g := range res.elements {
				prefixed[i] = map[string]any{
					res.name: g,
				}
			}
			slices = append(slices, prefixed)
			continue
		}
		slices = append(slices, res.elements)
	}

	results := []map[string]any{}
	// make([]int, len(slices)) creates an slice with elements for all slice
	// elements, with the default value 0.
	//
	// the next indexes are calculated in nextIndex, and this code
	// indexes[0] < lenSliceN(0, slices) guards against overflowing the index in
	// the slices.
	//
	// Each item is merged into a temporary map, and we check for already
	// existing maps so that we don't duplicate generated results.
	for indexes := make([]int, len(slices)); indexes[0] < lenSliceN(0, slices); nextIndex(indexes, slices) {
		temp := map[string]any{}
		for j, k := range indexes {
			if err := mergo.Merge(&temp, slices[j][k], mergo.WithOverride); err != nil {
				return nil, err
			}
		}

		if !alreadyExists(temp, results) {
			results = append(results, temp)
		}
	}

	return results, nil
}

func lenSliceN[T any](n int, slices [][]T) int {
	return len(slices[n])
}

func alreadyExists[T any](newMap T, existing []T) bool {
	for _, m := range existing {
		if reflect.DeepEqual(m, newMap) {
			return true
		}
	}

	return false
}

// populate the slice ix with the "next item" to get in each of the provided
// slices.
//
// for example, if you have...
//
// slice1: [eggs: 6, milk: 2, cheese: 1]
// slice2: [bag: 1]
//
// We use the first item in the cartesian call, [0 0] which is eggs.
// we call nextIndex to get the next item to use for the state [0 0].
// And we calculate [1 0] because the next item should be item 1 (milk) within
// the first slice, and item 0 (bag) in the second slice.
//
// Second time around the loop we pass in [1 0] as the current positions, and we
// calculate [2 0] because the next item in slice1 is cheese and item 0 in slice2 again.
//
// Finally we pass in [2 0] and we calculate [3 0], this is greater than the
// number of items in the primary slice.
//
// If we add a second element to slice2 the sequence will be longer.
//
// first item in slice 1 and each item from slice 2 i.e. [0 0], [0 1]
// second item in slice 1 and each item from slice 2 [1 0], [1 1]
// third item in slice 1 and each item from slice 2 [2 0], [2 1]
// and finally the [3 0] case which lets us exit the calculator.
func nextIndex[T any](ix []int, slices [][]T) {
	for j := len(ix) - 1; j >= 0; j-- {
		ix[j]++

		if j == 0 || ix[j] < lenSliceN(j, slices) {
			return
		}

		ix[j] = 0
	}
}

// singleElement flattens a set of generated elements into a single element.
func singleElement(generated []generatedElements) ([]map[string]any, error) {
	if len(generated) == 0 {
		return []map[string]any{}, nil
	}

	result := map[string]any{}
	for _, g := range generated {
		if g.name != "" {
			result[g.name] = []map[string]any{}
		}
	}

	unnamed := []map[string]any{}

	for _, generator := range generated {
		for _, element := range generator.elements {
			if generator.name == "" {
				unnamed = append(unnamed, element)
				continue
			}

			result[generator.name] = append(result[generator.name].([]map[string]any), element)
		}
	}

	if len(unnamed) > 0 {
		result["Matrix"] = unnamed
	}

	return []map[string]any{result}, nil
}
