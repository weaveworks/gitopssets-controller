package generators

import (
	"fmt"
	"reflect"
)

// GeneratorNotEnabledError is returned when a generator is not enabled
// in the controller but a GitOpsSet tries to use it.
// If you want to handle this error you can either use
// errors.As(err, &GeneratorNotEnabledError{}) to check for any generator, or use
// errors.Is(err, GeneratorNotEnabledError{Name: Matrix}) for a specific generator.
type GeneratorNotEnabledError struct {
	Name string
}

func (g GeneratorNotEnabledError) Error() string {
	return fmt.Sprintf("generator %s not enabled", g.Name)
}

// FindRelevantGenerators takes a struct with keys of the same type as
// Generators in the map and finds relevant generators.
func FindRelevantGenerators(setGenerator any, enabledGenerators map[string]Generator) ([]Generator, error) {
	res := []Generator{}
	v := reflect.Indirect(reflect.ValueOf(setGenerator))
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.CanInterface() {
			continue
		}

		if !reflect.ValueOf(field.Interface()).IsNil() {
			generatorName := v.Type().Field(i).Name
			gen, ok := enabledGenerators[generatorName]
			if !ok {
				return nil, GeneratorNotEnabledError{Name: generatorName}
			}
			res = append(res, gen)
			continue
		}
	}

	return res, nil
}
