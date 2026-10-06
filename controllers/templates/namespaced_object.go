package templates

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

// IsNamespacedObject returns true if the provided Object's Kind is a resource
// that is Namespaced.
//
// TODO: This should get the CRD for custom CRs but this requires privilege.
// https://kubernetes.io/docs/reference/using-api/api-concepts/#resource-uris
func IsNamespacedObject(obj runtime.Object) bool {
	return kind(obj) != "Namespace"
}

// ObjectIsNamespaced reports whether obj is a namespaced resource.
// A nil mapper keeps the historical kind check.
func ObjectIsNamespaced(mapper meta.RESTMapper, obj runtime.Object) (bool, error) {
	if mapper == nil {
		return IsNamespacedObject(obj), nil
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return false, fmt.Errorf("failed to determine if %s is namespaced: %w", gvk.Kind, err)
	}
	return mapping.Scope.Name() == meta.RESTScopeNameNamespace, nil
}

func kind(o runtime.Object) string {
	return o.GetObjectKind().GroupVersionKind().Kind
}
