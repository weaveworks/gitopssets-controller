package templates

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

func filterElements(generatorIndex int, expression string, elements []map[string]any) ([]map[string]any, error) {
	if expression == "" || len(elements) == 0 {
		return elements, nil
	}

	env, err := cel.NewEnv(
		cel.Variable("element", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return nil, fmt.Errorf("generator %d: %w", generatorIndex, err)
	}
	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("generator %d: %w", generatorIndex, issues.Err())
	}
	if !ast.OutputType().IsExactType(types.BoolType) {
		return nil, fmt.Errorf("generator %d: filter must return a boolean", generatorIndex)
	}
	program, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("generator %d: %w", generatorIndex, err)
	}

	kept := make([]map[string]any, 0, len(elements))
	for _, element := range elements {
		out, _, err := program.Eval(map[string]any{"element": element})
		if err != nil {
			return nil, fmt.Errorf("generator %d: %w", generatorIndex, err)
		}
		matches, ok := out.Value().(bool)
		if !ok {
			return nil, fmt.Errorf("generator %d: filter must return a boolean", generatorIndex)
		}
		if matches {
			kept = append(kept, element)
		}
	}
	return kept, nil
}
