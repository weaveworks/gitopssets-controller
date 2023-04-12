package generators

import (
	"fmt"
	"reflect"
)

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
