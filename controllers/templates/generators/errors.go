package generators

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NoArtifactError indicates that a Repository's artifact is not available.
type NoArtifactError struct {
	Kind string
	Name client.ObjectKey
}

func (e NoArtifactError) Error() string {
	return fmt.Sprintf("no artifact for %s %s", e.Kind, e.Name)
}

// ArtifactError creates and returns a new Artifact error.
func ArtifactError(kind string, name client.ObjectKey) error {
	return NoArtifactError{
		Kind: kind,
		Name: name,
	}
}
