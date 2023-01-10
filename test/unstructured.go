package test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// ToUnstructured converts a k8s Object to an Unstructured.
//
// It also removes the "status" field from the Unstructured representation.
func ToUnstructured(t *testing.T, obj runtime.Object) *unstructured.Unstructured {
	t.Helper()
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	AssertNoError(t, err)

	delete(raw, "status")

	return &unstructured.Unstructured{Object: raw}
}
