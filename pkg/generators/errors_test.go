package generators

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestArtifactError(t *testing.T) {
	err := ArtifactError("GitRepository", client.ObjectKey{Namespace: "demo", Name: "app"})
	if err.Error() != "no artifact for GitRepository demo/app" {
		t.Fatalf("error = %q", err.Error())
	}
	if _, ok := err.(NoArtifactError); !ok {
		t.Fatalf("error type = %T", err)
	}
}
