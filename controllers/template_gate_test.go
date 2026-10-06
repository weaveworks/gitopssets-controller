package controllers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

func TestTemplateGateHoldsTheRemoteKustomization(t *testing.T) {
	cl, gens := rolloutClient(t)
	counter := &countingPatchClient{Client: cl}
	gs := gatedRuntimeSet("pp", "prod")
	reconciler := &GitOpsSetReconciler{}
	stats := &applyStats{}
	ctx := context.WithValue(t.Context(), applyStatsKey, stats)

	inventory, err := reconciler.renderAndReconcile(ctx, logr.Discard(), counter, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Entries) != 4 {
		t.Fatalf("entries = %#v", inventoryIDs(inventory))
	}
	if objectExists(t, cl, "flux-system", "runtime-pp", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") || objectExists(t, cl, "flux-system", "runtime-prod", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("remote kustomization was applied before the claim was ready")
	}
	if !objectExists(t, cl, "flux-system", "runtime-pp-kubeconfig", "v1", "ConfigMap") || !objectExists(t, cl, "crossplane-system", "pp", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("claim or kubeconfig was not applied")
	}
	if len(stats.waiting) != 2 {
		t.Fatalf("waiting = %#v", stats.waiting)
	}

	gs = gitOpsSetWithInventory(gs, inventory)
	before := counter.patches
	if _, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens); err != nil {
		t.Fatal(err)
	}
	if counter.patches != before {
		t.Fatalf("patches while waiting = %d", counter.patches-before)
	}

	setReadyCondition(t, cl, "crossplane-system", "pp", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization", true)
	inventory, err = reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	if !objectExists(t, cl, "flux-system", "runtime-pp", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("runtime-pp was not applied")
	}
	if objectExists(t, cl, "flux-system", "runtime-prod", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("runtime-prod was applied before its claim was ready")
	}

	gs = gitOpsSetWithInventory(gs, inventory)
	setReadyCondition(t, cl, "crossplane-system", "prod", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization", true)
	inventory, err = reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	if !objectExists(t, cl, "flux-system", "runtime-prod", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("runtime-prod was not applied")
	}

	before = counter.patches
	setReadyCondition(t, cl, "crossplane-system", "pp", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization", false)
	gs = gitOpsSetWithInventory(gs, inventory)
	inventory, err = reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	if !objectExists(t, cl, "flux-system", "runtime-pp", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("runtime-pp was deleted when the claim became not ready")
	}
	if counter.patches != before {
		t.Fatalf("patches after claim flipped = %d", counter.patches-before)
	}

	gs = gitOpsSetWithInventory(gs, inventory)
	gs.Spec.Generators[0].List.Elements = gs.Spec.Generators[0].List.Elements[:1]
	if _, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens); err != nil {
		t.Fatal(err)
	}
	if objectExists(t, cl, "crossplane-system", "prod", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") || objectExists(t, cl, "flux-system", "runtime-prod", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") || objectExists(t, cl, "flux-system", "runtime-prod-kubeconfig", "v1", "ConfigMap") {
		t.Fatal("prod objects remained after the element was removed")
	}
}

func TestTemplateGateDoesNotTreatAConfigMapAsReady(t *testing.T) {
	cl, gens := rolloutClient(t)
	gs := gatedRuntimeSet("pp")
	gs.Spec.Templates[2].Requires = []string{"kubeconfig"}
	inventory, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	if objectExists(t, cl, "flux-system", "runtime-pp", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("kustomization was applied while waiting on a ConfigMap")
	}
	if len(inventory.Entries) != 2 {
		t.Fatalf("entries = %#v", inventoryIDs(inventory))
	}
}

func TestUnknownTemplateRequirementFailsBeforeApply(t *testing.T) {
	cl, gens := rolloutClient(t)
	counter := &countingPatchClient{Client: cl}
	gs := gatedRuntimeSet("pp")
	gs.Spec.Templates[2].Requires = []string{"missing"}
	_, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens)
	if err == nil || !strings.Contains(err.Error(), "unknown template") {
		t.Fatalf("err = %v", err)
	}
	if counter.patches != 0 {
		t.Fatalf("patches = %d", counter.patches)
	}
}

func TestHeldObjectDoesNotConsumeRolloutBudget(t *testing.T) {
	cl, gens := rolloutClient(t)
	maxResources := 1
	gs := gatedRuntimeSet("pp")
	gs.Spec.Rollout = &templatesv1.Rollout{MaxResources: &maxResources}
	inventory, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	assertRollout(t, err, 1, 1)
	if len(inventory.Entries) != 1 {
		t.Fatalf("entries = %#v", inventoryIDs(inventory))
	}
	if objectExists(t, cl, "flux-system", "runtime-pp", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("held kustomization consumed the rollout slot")
	}
}

func TestDuplicateTemplateNameFails(t *testing.T) {
	cl, gens := rolloutClient(t)
	gs := gatedRuntimeSet("pp")
	gs.Spec.Templates[1].Name = "claim"
	_, err := (&GitOpsSetReconciler{}).renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err == nil || !strings.Contains(err.Error(), "duplicate template name") {
		t.Fatalf("err = %v", err)
	}
}

func gatedRuntimeSet(envs ...string) *templatesv1.GitOpsSet {
	projects := map[string]string{"pp": "pw-runtime-pp", "prod": "pw-runtime-prod"}
	raws := make([]apiextensionsv1.JSON, 0, len(envs))
	for _, env := range envs {
		raw, err := json.Marshal(map[string]string{"env": env, "project": projects[env], "region": "us-east4"})
		if err != nil {
			panic(err)
		}
		raws = append(raws, apiextensionsv1.JSON{Raw: raw})
	}
	return &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "flux-system"},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{{
				List: &templatesv1.ListGenerator{Elements: raws},
			}},
			Templates: []templatesv1.GitOpsSetTemplate{
				{Name: "claim", Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"kustomize.toolkit.fluxcd.io/v1beta2","kind":"Kustomization","metadata":{"name":"{{ .Element.env }}","namespace":"crossplane-system"},"spec":{"interval":"10m","path":"./clusters/runtime/{{ .Element.env }}","prune":true,"sourceRef":{"kind":"GitRepository","name":"flux-system"}}}`)}},
				{Name: "kubeconfig", Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"runtime-{{ .Element.env }}-kubeconfig","namespace":"flux-system"},"data":{"provider":"gcp","cluster":"projects/{{ .Element.project }}/locations/{{ .Element.region }}/clusters/runtime-{{ .Element.env }}"}}`)}},
				{Name: "apply", Requires: []string{"claim"}, Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"kustomize.toolkit.fluxcd.io/v1beta2","kind":"Kustomization","metadata":{"name":"runtime-{{ .Element.env }}","namespace":"flux-system"},"spec":{"interval":"10m","path":"./clusters/runtime/{{ .Element.env }}","prune":true,"wait":true,"sourceRef":{"kind":"GitRepository","name":"flux-system"},"kubeConfig":{"configMapRef":{"name":"runtime-{{ .Element.env }}-kubeconfig"}}}}`)}},
			},
		},
	}
}

func objectExists(t *testing.T, cl client.Client, namespace, name, apiVersion, kind string) bool {
	t.Helper()
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	return cl.Get(t.Context(), types.NamespacedName{Namespace: namespace, Name: name}, obj) == nil
}

func setReadyCondition(t *testing.T, cl client.Client, namespace, name, apiVersion, kind string, ready bool) {
	t.Helper()
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
		t.Fatal(err)
	}
	status := "False"
	if ready {
		status = "True"
	}
	if err := unstructured.SetNestedSlice(obj.Object, []any{map[string]any{"type": "Ready", "status": status}}, "status", "conditions"); err != nil {
		t.Fatal(err)
	}
	if err := cl.Update(t.Context(), obj); err != nil {
		t.Fatal(err)
	}
}
