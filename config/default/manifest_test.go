package default_test

import (
	"strings"
	"testing"

	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

func TestDefaultInstallHasNoMetricsProxy(t *testing.T) {
	rendered, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(filesys.MakeFsOnDisk(), ".")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := rendered.AsYaml()
	if err != nil {
		t.Fatal(err)
	}
	text := string(manifest)
	for _, forbidden := range []string{"kube-rbac-proxy", "gcr.io/kubebuilder", "containerPort: 8443"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("default install still contains %s", forbidden)
		}
	}
	if !strings.Contains(text, "--metrics-bind-address=127.0.0.1:8080") {
		t.Fatal("manager metrics are not bound to localhost")
	}
	if strings.Count(text, "name: manager") < 1 {
		t.Fatal("manager container is missing")
	}
}
