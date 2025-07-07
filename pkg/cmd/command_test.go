package cmd

import (
	"strings"
	"testing"

	"github.com/gitops-tools/gitopssets-controller/pkg/setup"
	"github.com/google/go-cmp/cmp"
)

func TestRenderGitOpsSet(t *testing.T) {
	var out strings.Builder

	err := renderGitOpsSet("testdata/list_set.yaml", setup.DefaultGenerators, true, "", &out)
	if err != nil {
		t.Fatal(err)
	}

	want := `---
apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
kind: Kustomization
metadata:
  labels:
    app.kubernetes.io/instance: dev
    app.kubernetes.io/name: go-demo
    com.example/team: dev-team
    sets.gitops.pro/name: gitopsset-sample
    sets.gitops.pro/namespace: ""
  name: dev-demo
  namespace: default
spec:
  interval: 5m
  path: ./examples/kustomize/environments/dev
  prune: true
  sourceRef:
    kind: GitRepository
    name: go-demo-repo
`
	if diff := cmp.Diff(want, out.String()); diff != "" {
		t.Fatalf("failed to generate:\n%s", diff)
	}
}

func TestRenderGitOpsSet_with_multiple_sets(t *testing.T) {
	var out strings.Builder

	err := renderGitOpsSet("testdata/list_sets.yaml", setup.DefaultGenerators, true, "", &out)
	if err != nil {
		t.Fatal(err)
	}

	want := `---
apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
kind: Kustomization
metadata:
  labels:
    app.kubernetes.io/instance: dev
    app.kubernetes.io/name: go-demo
    com.example/team: dev-team
    sets.gitops.pro/name: gitopsset-sample
    sets.gitops.pro/namespace: ""
  name: dev-demo
  namespace: default
spec:
  interval: 5m
  path: ./examples/kustomize/environments/dev
  prune: true
  sourceRef:
    kind: GitRepository
    name: go-demo-repo
---
apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
kind: Kustomization
metadata:
  labels:
    app.kubernetes.io/instance: dev
    app.kubernetes.io/name: go-demo
    com.example/team: dev-team
    sets.gitops.pro/name: second-gitopsset-sample
    sets.gitops.pro/namespace: ""
  name: dev-demo-2
  namespace: default
spec:
  interval: 5m
  path: ./examples/kustomize/environments/dev
  prune: true
  sourceRef:
    kind: GitRepository
    name: go-demo-repo
`
	if diff := cmp.Diff(want, out.String()); diff != "" {
		t.Fatalf("failed to generate:\n%s", diff)
	}
}
