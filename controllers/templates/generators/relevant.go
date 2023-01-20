package generators

import (
	"reflect"
)

// FindRelevantGenerators takes a struct with keys of the same type as
// Generators in the map and finds relevant generators.
func FindRelevantGenerators(setGenerator any, allGenerators map[string]Generator) []Generator {
	res := []Generator{}
	v := reflect.Indirect(reflect.ValueOf(setGenerator))
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.CanInterface() {
			continue
		}

		if !reflect.ValueOf(field.Interface()).IsNil() {
			gen, ok := allGenerators[v.Type().Field(i).Name]
			if ok {
				res = append(res, gen)
				continue
			}
		}
	}

	return res
}
