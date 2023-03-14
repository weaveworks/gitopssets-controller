package v1alpha1

import (
	"fmt"

	runtime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// ResourceInventory contains a list of Kubernetes resource object references that have been applied by a Kustomization.
type ResourceInventory struct {
	// Entries of Kubernetes resource object references.
	Entries []ResourceRef `json:"entries,omitempty"`
}

// ResourceRef contains the information necessary to locate a resource within a cluster.
type ResourceRef struct {
	// ID is the string representation of the Kubernetes resource object's metadata,
	// in the format '<namespace>_<name>_<group>_<kind>'.
	ID string `json:"id"`

	// Version is the API version of the Kubernetes resource object's kind.
	Version string `json:"v"`
}

// ResourceRefFromObject returns a ResourceRef from a runtime.Object.
func ResourceRefFromObject(obj runtime.Object) (ResourceRef, error) {
	objMeta, err := object.RuntimeToObjMeta(obj)
	if err != nil {
		return ResourceRef{}, fmt.Errorf("failed to parse object Metadata: %w", err)
	}

	return ResourceRef{
		ID:      objMeta.String(),
		Version: obj.GetObjectKind().GroupVersionKind().Version,
	}, nil
}
