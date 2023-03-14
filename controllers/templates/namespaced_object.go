package templates

import (
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

func kind(o runtime.Object) string {
	return o.GetObjectKind().GroupVersionKind().Kind
}
